// utils/extract-error.js — Consistent error message extraction.
//
// Backend errors arrive in multiple shapes depending on the endpoint:
//   - { error: "message" }       (most /api handlers)
//   - { message: "message" }     (some handlers)
//   - ApiError.message           (thrown by api.js for !res.ok)
//   - Error.message              (network errors, thrown by fetch)
//   - string                     (raw error)
//
// This utility normalizes all of them into a single string.

/**
 * Extract a human-readable error message from any error value.
 *
 * @param {unknown} err - The error value (Error, object, string, etc.)
 * @returns {string} The extracted message, or 'Unknown error' if none found.
 */
export function extractError(err) {
    if (!err) return 'Unknown error';
    if (typeof err === 'string') return err;
    if (err instanceof Error) return err.message;
    // ApiError has .message and .data; plain objects may have .error or .message
    if (typeof err === 'object') {
        return err.message || err.error || err.statusText || 'Unknown error';
    }
    return String(err);
}
