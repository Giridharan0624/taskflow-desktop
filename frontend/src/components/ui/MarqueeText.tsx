import { useEffect, useRef, useState } from "preact/hooks"
import { cn } from "../../lib/cn"

interface MarqueeTextProps {
  /** Text to display; scrolls only if its rendered width exceeds the container. */
  children: string
  /** Scroll speed in pixels per second. Default 40 — slow enough to read, fast
   *  enough to not feel tedious. */
  speed?: number
  /** Pause (seconds) between loop cycles so the user can catch up if they look
   *  away. Default 2. */
  pause?: number
  class?: string
  className?: string
  title?: string
}

/**
 * Horizontally scrolling ticker for single-line text that overflows its
 * container. Measures via ResizeObserver and only animates when needed:
 * short labels render as plain text with no animation cost.
 *
 * Implementation: when overflowing, we render two copies of the text
 * separated by a fixed gap. The outer container translates by
 * -(first copy width + gap) in one linear cycle; the second copy
 * seamlessly takes the first's position at loop boundary, so the
 * transition is invisible. This is the standard CSS-only marquee
 * technique — no JS timers, no layout thrash.
 */
export function MarqueeText({
  children,
  speed = 40,
  pause = 2,
  class: cls,
  className,
  title,
}: MarqueeTextProps) {
  const hostRef = useRef<HTMLSpanElement>(null)
  const textRef = useRef<HTMLSpanElement>(null)
  const [metrics, setMetrics] = useState<{ overflow: boolean; duration: number }>({
    overflow: false,
    duration: 0,
  })

  useEffect(() => {
    if (!hostRef.current || !textRef.current) return
    const host = hostRef.current
    const text = textRef.current

    const measure = () => {
      const textWidth = text.scrollWidth
      const hostWidth = host.clientWidth
      if (textWidth <= hostWidth + 1) {
        setMetrics({ overflow: false, duration: 0 })
        return
      }
      // Duration = distance / speed. Distance = one full text-width so
      // the second copy takes exactly one cycle to replace the first.
      const duration = Math.max(3, textWidth / speed) + pause
      setMetrics({ overflow: true, duration })
    }

    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(host)
    ro.observe(text)
    return () => ro.disconnect()
  }, [children, speed, pause])

  // The `--mq-dur` CSS variable drives the keyframe duration. The
  // keyframe itself lives in main.css so the animation stays
  // declarative (no inline <style> thrash on every render).
  const style = metrics.overflow
    ? ({ "--mq-dur": `${metrics.duration}s` } as Record<string, string>)
    : undefined

  return (
    <span
      ref={hostRef}
      class={cn(
        "inline-block w-full overflow-hidden whitespace-nowrap align-middle",
        cls,
        className,
      )}
      title={title ?? children}
    >
      {metrics.overflow ? (
        <span class="inline-flex gap-8 animate-marquee" style={style}>
          {/* Two copies with a gap — when the first scrolls out, the
              second is already sitting where the first started, so
              there's no visible jump at loop boundary. aria-hidden on
              the duplicate keeps screen readers from announcing the
              text twice. */}
          <span ref={textRef} class="shrink-0">
            {children}
          </span>
          <span aria-hidden="true" class="shrink-0">
            {children}
          </span>
        </span>
      ) : (
        <span ref={textRef} class="inline-block">
          {children}
        </span>
      )}
    </span>
  )
}
