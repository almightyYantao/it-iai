// copyText writes the given string to the system clipboard.
//
// Two reasons we don't just call navigator.clipboard.writeText directly:
//   1. navigator.clipboard is only defined in a "secure context" — HTTPS or
//      http://localhost. Hitting the admin UI over plain HTTP via an ECS IP
//      (the common dev / internal-network case) leaves `navigator.clipboard`
//      undefined, so the call throws and the button looks broken.
//   2. Some browser extensions / iframe sandboxes block the modern API even
//      under HTTPS.
//
// We try the modern API first (the only path that works in cross-origin
// iframes and tracks Permissions correctly), then fall back to the
// legacy textarea + execCommand("copy") which is HTTP-safe but synchronous
// and triggers a "deprecated" console warning.
//
// Returns true on success so callers can flash a "copied" state.
export async function copyText(text: string): Promise<boolean> {
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the legacy path
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    // Off-screen but selectable; avoids scrolling and visual flash.
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "0";
    ta.style.left = "0";
    ta.style.opacity = "0";
    ta.style.pointerEvents = "none";
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, ta.value.length);
    const ok = document.execCommand("copy");
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}
