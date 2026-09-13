// User preferences, stored in this browser only.
//
// These affect presentation exclusively. Times and dates arrive from the API in
// UTC and are stored that way; nothing here changes what is sent to or read
// from the database.

export type Timezone = "utc" | "browser";
export type TimeFormat = "24" | "12";
export type DateFormat = "iso" | "dmy" | "mdy" | "dmy-dot";

export type Settings = {
  timezone: Timezone;
  timeFormat: TimeFormat;
  dateFormat: DateFormat;
};

export const DEFAULT_SETTINGS: Settings = {
  timezone: "utc",
  timeFormat: "24",
  dateFormat: "iso",
};

export const DATE_FORMAT_OPTIONS: { value: DateFormat; label: string; example: string }[] = [
  { value: "iso", label: "Year first", example: "2026-02-14" },
  { value: "dmy", label: "Day first", example: "14/02/2026" },
  { value: "dmy-dot", label: "Day first, dots", example: "14.02.2026" },
  { value: "mdy", label: "Month first", example: "02/14/2026" },
];

const STORAGE_KEY = "sky-history-settings";

// Read by the formatters. Kept as module state so formatting helpers stay
// callable without threading settings through every component.
let current: Settings = DEFAULT_SETTINGS;

export function getSettings(): Settings {
  return current;
}

export function loadSettings(): Settings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Settings>;
      current = {
        timezone: parsed.timezone === "browser" ? "browser" : "utc",
        timeFormat: parsed.timeFormat === "12" ? "12" : "24",
        dateFormat: ["iso", "dmy", "mdy", "dmy-dot"].includes(parsed.dateFormat as string)
          ? (parsed.dateFormat as DateFormat)
          : "iso",
      };
    }
  } catch {
    // Private browsing or blocked storage: fall back to defaults.
    current = DEFAULT_SETTINGS;
  }
  return current;
}

export function saveSettings(next: Settings): Settings {
  current = next;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Not persisting is survivable; the setting still applies for this session.
  }
  return current;
}

/** The IANA zone name for the browser, e.g. "Europe/Stockholm". */
export function browserZoneName(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "Local time";
  } catch {
    return "Local time";
  }
}

/**
 * Describes the zone times are currently shown in, for a hover tooltip.
 * Times are never labelled inline -- the tooltip is the only place this appears.
 */
export function timezoneTooltip(): string {
  if (current.timezone === "utc") return "Times shown in UTC";
  const offset = -new Date().getTimezoneOffset();
  const sign = offset < 0 ? "-" : "+";
  const abs = Math.abs(offset);
  const hh = String(Math.floor(abs / 60)).padStart(2, "0");
  const mm = String(abs % 60).padStart(2, "0");
  return `Times shown in ${browserZoneName()} (UTC${sign}${hh}:${mm})`;
}
