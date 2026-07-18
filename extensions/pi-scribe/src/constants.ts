export const SCRIBE_URL = process.env["SCRIBE_URL"] ?? "http://127.0.0.1:8080";
export const SCRIBE_AUTH_TOKEN = process.env["SCRIBE_AUTH_TOKEN"] ?? "";
export const SCRIBE_TIMEOUT_MS = Number(process.env["SCRIBE_TIMEOUT_MS"] ?? 15_000);
export const VERSION = "0.1.0";
