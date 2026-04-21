import type { JSX, ComponentChildren } from "preact"
import { cn } from "../../lib/cn"

type DivProps = JSX.HTMLAttributes<HTMLDivElement> & { children?: ComponentChildren }

export function Card({ className, children, ...rest }: DivProps) {
  return (
    <div
      class={cn(
        "rounded-lg border border-border bg-card text-card-foreground shadow-sm",
        className as string | undefined,
      )}
      {...rest}
    >
      {children}
    </div>
  )
}

export function CardHeader({ className, children, ...rest }: DivProps) {
  return (
    <div class={cn("flex flex-col space-y-1.5 p-4", className as string | undefined)} {...rest}>
      {children}
    </div>
  )
}

export function CardTitle({ className, children, ...rest }: DivProps) {
  return (
    <h3
      class={cn(
        "text-base font-semibold leading-none tracking-tight",
        className as string | undefined,
      )}
      {...(rest as JSX.HTMLAttributes<HTMLHeadingElement>)}
    >
      {children}
    </h3>
  )
}

export function CardDescription({ className, children, ...rest }: DivProps) {
  return (
    <p
      class={cn("text-xs text-muted-foreground", className as string | undefined)}
      {...(rest as JSX.HTMLAttributes<HTMLParagraphElement>)}
    >
      {children}
    </p>
  )
}

export function CardContent({ className, children, ...rest }: DivProps) {
  return (
    <div class={cn("p-4 pt-0", className as string | undefined)} {...rest}>
      {children}
    </div>
  )
}

export function CardFooter({ className, children, ...rest }: DivProps) {
  return (
    <div
      class={cn("flex items-center p-4 pt-0", className as string | undefined)}
      {...rest}
    >
      {children}
    </div>
  )
}
