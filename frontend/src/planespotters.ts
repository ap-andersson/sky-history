// Aircraft photos from planespotters.net's public API, fetched the same way
// tar1090 does it: a plain client-side GET keyed on the ICAO hex, refined with
// registration and type once known, no API key or backend involved.
//
// Their bot filter rejects generic HTTP-library user agents (curl,
// python-requests and the like) but passes an ordinary browser fine -- this
// runs entirely in the browser, so there is nothing to configure for that.
// CORS is open (`Access-Control-Allow-Origin: *`), confirmed against the live
// API before writing this.
//
// Every photo shown must carry the photographer's credit and link back to its
// page on planespotters.net -- see https://www.planespotters.net/photo/api.
// AircraftPhotoCredit in App.tsx and the click-through link are how this
// module's caller satisfies that; this file only fetches and shapes the data.

export interface AircraftPhoto {
  thumbnailUrl: string;
  link: string;
  photographer: string;
}

const API_BASE = "https://api.planespotters.net/pub/photos/hex/";

// One fetch per aircraft for the life of the tab. The photo does not change
// while flipping between dates for the same ICAO, so a plain in-memory cache
// (never persisted, never expired) is all this needs -- there is nothing to
// invalidate short of the aircraft getting a new paint job mid-session.
const cache = new Map<string, Promise<AircraftPhoto | null>>();

/**
 * Looks up a photo for the given aircraft. Registration and type refine the
 * match when known; both are optional. Resolves to null -- never rejects --
 * whenever no photo is found or the lookup fails for any reason: a missing
 * photo is a normal, common outcome, not an error worth surfacing.
 */
export function getAircraftPhoto(
  icao: string,
  registration?: string,
  typeCode?: string
): Promise<AircraftPhoto | null> {
  const key = icao.toUpperCase();
  const cached = cache.get(key);
  if (cached) return cached;

  const promise = fetchPhoto(key, registration, typeCode);
  cache.set(key, promise);
  return promise;
}

async function fetchPhoto(
  icao: string,
  registration?: string,
  typeCode?: string
): Promise<AircraftPhoto | null> {
  // Non-ICAO addresses (TIS-B, ADS-R, and the like) start with ~ and were
  // never assigned to a real airframe, so there is nothing to look up.
  if (icao.startsWith("~")) return null;

  const params = new URLSearchParams();
  if (registration) params.set("reg", registration);
  if (typeCode) params.set("icaoType", typeCode);
  const qs = params.toString();

  try {
    const resp = await fetch(`${API_BASE}${icao}${qs ? `?${qs}` : ""}`);
    if (!resp.ok) return null;

    const data = await resp.json();
    const photo = data?.photos?.[0];
    if (!photo) return null;

    // thumbnail_large is what tar1090 displays too: already sized for a small
    // preview (fixed 280px height, width varies with the photo's own aspect
    // ratio), rather than pulling the full-resolution original just to shrink
    // it back down in CSS.
    const thumbnailUrl: string | undefined =
      photo.thumbnail_large?.src || photo.thumbnail?.src;
    if (!thumbnailUrl || !photo.link) return null;

    return {
      thumbnailUrl,
      link: photo.link,
      photographer: photo.photographer || "Unknown",
    };
  } catch {
    // Network hiccup, CORS, rate limit -- all treated as "no photo found".
    // This is a nice-to-have next to the archive data, not something to
    // retry or report.
    return null;
  }
}
