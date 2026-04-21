import type { JSX } from "preact"
import { cn } from "../../lib/cn"

// JSX.IntrinsicElements['label'] carries `for` / `htmlFor`; the
// generic JSX.HTMLAttributes<HTMLLabelElement> does not.
type LabelProps = JSX.IntrinsicElements["label"]

export function Label({ className, children, ...rest }: LabelProps) {
  return (
    <label
      class={cn(
        "text-xs font-medium leading-none",
        "peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
        className as string | undefined,
      )}
      {...rest}
    >
      {children}
    </label>
  )
}
