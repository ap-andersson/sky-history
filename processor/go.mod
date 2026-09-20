module github.com/sky-history/processor

go 1.22.0

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/sky-history/shared v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

// The shared module is not published; it lives beside this one in the repo.
// This is why the Docker build context is the repository root -- see
// shared/README.md.
replace github.com/sky-history/shared => ../shared
