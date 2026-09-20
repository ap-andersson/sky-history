// Sky-History collector.
//
// Fills the gap between the end of the last processed adsb.lol release and
// now, by polling approved ADS-B feeders for their aircraft.json and writing
// live flight rows. Those rows are provisional: once the archive covering
// their date is ingested, the processor deletes them and the archive takes
// over. See db/migrations/006_live_feeders.sql.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sky-history/collector/collect"
	"github.com/sky-history/collector/config"
	"github.com/sky-history/collector/db"
	"github.com/sky-history/shared/feedcheck"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Sky-History Collector starting...")

	cfg := config.Load()

	if !cfg.EnableLiveGapFill {
		// Idle rather than exit: the container stays in the stack, and turning
		// the feature on is a restart away rather than a compose edit. A
		// crash-looping container would also bury the reason in noise.
		log.Println("Live gap-fill is off, so no feeders will be polled. " +
			"Set ENABLE_LIVE_GAPFILL=true to turn it on.")
		waitForSignal()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutdown signal received, flushing open segments...")
		cancel()
	}()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := waitForSchema(ctx, pool); err != nil {
		log.Fatalf("Schema never became ready: %v", err)
	}

	if cfg.AllowPrivateFeeders {
		log.Println("WARNING: ALLOW_PRIVATE_FEEDERS is on. Feeders may point at private " +
			"and loopback addresses, so only run this where the submit form is not " +
			"reachable by the public.")
	}

	c := &Collector{
		cfg:        cfg,
		feederRepo: db.NewFeederRepo(pool),
		flightRepo: db.NewFlightRepo(pool),
		client:     feedcheck.NewClient(cfg.FetchTimeout, cfg.AllowPrivateFeeders),
		aggregator: collect.NewAggregator(cfg.SegmentGap),
		sightings:  make(chan []collect.Sighting, 256),
		results:    make(chan db.PollResult, 256),
		pollers:    make(map[int]context.CancelFunc),
	}

	c.Run(ctx)
	log.Println("Collector stopped.")
}

// Collector owns the aggregator and is the only goroutine that touches it.
// Feeders reach it over channels, which keeps the cross-feeder merge free of
// locks no matter how many feeders are approved.
type Collector struct {
	cfg        config.Config
	feederRepo *db.FeederRepo
	flightRepo *db.FlightRepo
	client     *feedcheck.Client
	aggregator *collect.Aggregator

	sightings chan []collect.Sighting
	results   chan db.PollResult

	pollers   map[int]context.CancelFunc
	pending   []db.PollResult
	flushing  bool
	flushDone chan int
}

func (c *Collector) Run(ctx context.Context) {
	c.flushDone = make(chan int, 1)

	c.reloadFeeders(ctx)

	reloadTicker := time.NewTicker(c.cfg.FeederReloadInterval)
	defer reloadTicker.Stop()
	flushTicker := time.NewTicker(c.cfg.FlushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.shutdown()
			return

		case batch := <-c.sightings:
			for _, s := range batch {
				c.aggregator.Observe(s)
			}

		case res := <-c.results:
			c.pending = append(c.pending, res)

		case n := <-c.flushDone:
			c.flushing = false
			if n > 0 {
				log.Printf("Flushed %d live flight row(s); tracking %d aircraft", n, c.aggregator.OpenCount())
			}

		case <-reloadTicker.C:
			c.reloadFeeders(ctx)

		case <-flushTicker.C:
			c.aggregator.Sweep(time.Now().UTC())
			c.flush(ctx)
		}
	}
}

// flush hands the current batch to a background write. Draining is cheap and
// happens here; the database round trip does not, and running it inline would
// stall every feeder behind it.
func (c *Collector) flush(ctx context.Context) {
	if c.flushing {
		// The previous write is still going. Segments stay in the aggregator
		// and go out on the next tick, so nothing is lost by skipping.
		return
	}

	segments := c.aggregator.Drain()
	results := c.pending
	c.pending = nil

	if len(segments) == 0 && len(results) == 0 {
		return
	}

	c.flushing = true
	go func() {
		written := 0
		if len(segments) > 0 {
			n, err := c.flightRepo.Flush(ctx, segments)
			if err != nil {
				log.Printf("Error flushing live flights: %v", err)
			}
			written = n
		}
		if len(results) > 0 {
			if err := c.feederRepo.RecordPolls(ctx, results); err != nil {
				log.Printf("Error recording feeder health: %v", err)
			}
		}
		c.flushDone <- written
	}()
}

// reloadFeeders starts pollers for newly approved feeders and stops those that
// have been disabled or deleted.
func (c *Collector) reloadFeeders(ctx context.Context) {
	feeders, err := c.feederRepo.ListEnabled(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("Error loading feeder list: %v", err)
		}
		return
	}

	seen := make(map[int]bool, len(feeders))
	for _, f := range feeders {
		seen[f.ID] = true
		if _, running := c.pollers[f.ID]; running {
			continue
		}

		pollCtx, stop := context.WithCancel(ctx)
		c.pollers[f.ID] = stop

		p := collect.NewPoller(f, c.client, c.cfg.PollInterval, c.sightings, c.results)
		go p.Run(pollCtx)
	}

	for id, stop := range c.pollers {
		if !seen[id] {
			log.Printf("Feeder %d is no longer enabled; stopping", id)
			stop()
			delete(c.pollers, id)
		}
	}

	if len(feeders) == 0 {
		log.Println("No enabled feeders. Approve one by setting feeders.enabled = TRUE.")
	}
}

// shutdown closes every open segment and writes it, so a restart does not lose
// the flights currently in the air.
func (c *Collector) shutdown() {
	for _, stop := range c.pollers {
		stop()
	}

	c.aggregator.CloseAll()
	segments := c.aggregator.Drain()
	if len(segments) == 0 && len(c.pending) == 0 {
		return
	}

	// A fresh context: the one the collector ran under is already cancelled,
	// and this final write is the whole point of a graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if n, err := c.flightRepo.Flush(ctx, segments); err != nil {
		log.Printf("Error flushing on shutdown: %v", err)
	} else {
		log.Printf("Flushed %d live flight row(s) on shutdown", n)
	}
	if len(c.pending) > 0 {
		if err := c.feederRepo.RecordPolls(ctx, c.pending); err != nil {
			log.Printf("Error recording feeder health on shutdown: %v", err)
		}
	}
}

// waitForSchema blocks until the processor has applied the migration that adds
// the feeders table. On a fresh stack both containers start together and the
// collector usually wins that race, so it waits rather than failing.
func waitForSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for attempt := 0; ; attempt++ {
		ready, err := db.SchemaReady(ctx, pool)
		if err == nil && ready {
			return nil
		}
		if attempt == 0 {
			log.Println("Waiting for the processor to apply the live-feeder migration...")
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitForSignal blocks until the container is asked to stop.
func waitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
