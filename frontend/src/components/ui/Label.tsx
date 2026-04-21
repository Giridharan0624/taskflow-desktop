import type { JSX, ComponentChildren } from "preact"
import { cn } from "../../lib/cn"

interface LabelProps extends JSX.HTMLAttributes<HTMLLabelElement> {
  children?: ComponentChildren
}

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
