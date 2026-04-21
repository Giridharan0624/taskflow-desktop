import type { JSX } from "preact"
import { forwardRef } from "preact/compat"
import { cn } from "../../lib/cn"

type InputProps = JSX.HTMLAttributes<HTMLInputElement>

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className, ...rest }, ref) => {
    // type defaults to "text" in the browser when omitted — no need
    // to pass it explicitly. Pulling it out of rest would trip
    // Preact's narrow HTMLAttributes<HTMLInputElement>['type'] union.
    return (
      <input
        ref={ref}
        class={cn(
          "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm",
          "shadow-sm transition-colors",
          "placeholder:text-muted-foreground",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className as string | undefined,
        )}
        {...rest}
      />
    )
  },
)
Input.displayName = "Input"
