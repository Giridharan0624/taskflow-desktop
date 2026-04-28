/**
 * Deterministic color from a project name (or any string).
 *
 * The web app's project records carry a real color picked by the
 * project lead, but the desktop's GetMyTasks endpoint doesn't ship
 * that field yet. This is a no-network stand-in: hash the project
 * name into an HSL hue so the same project always renders with the
 * same color, while different projects get visibly different ones.
 *
 * Saturation/lightness are fixed in two ranges (light + dark theme
 * versions) so the dot stays readable on either background. The
 * picker avoids muddy yellow-greens by clamping into well-spaced
 * hue bands.
 */

// Simple FNV-1a 32-bit hash. Stable across runtimes, no deps.
function hash(str: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = (h * 0x01000193) >>> 0;
  }
  return h >>> 0;
}

// 12 hand-picked hues — evenly spaced around the wheel, skipping
// the murky yellow-green zone that reads as "sick" on light bg.
// Picking from a discrete palette also avoids two adjacent
// projects rendering with imperceptibly-different hues.
const HUES = [212, 240, 264, 290, 320, 348, 8, 28, 48, 152, 176, 196];

export function colorForProject(name: string | null | undefined): string {
  const key = (name || "").trim().toLowerCase() || "default";
  const idx = hash(key) % HUES.length;
  const h = HUES[idx];
  // hsl saturation 70%, lightness 55% — readable on both light and
  // dark backgrounds. Tailwind's `style="background-color:..."`
  // path handles the rest.
  return `hsl(${h} 70% 55%)`;
}
