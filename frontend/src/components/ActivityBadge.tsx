interface ActivityBadgeProps {
  idleSeconds: number;
}

/**
 * ActivityBadge — shows a color-coded activity indicator.
 * Green: active (idle < 60s)
 * Yellow: slightly idle (60s - 180s)
 * Red: idle (> 180s)
 */
export function ActivityBadge({ idleSeconds }: ActivityBadgeProps) {
  const color =
    idleSeconds < 60
      ? "bg-emerald-500"
      : idleSeconds < 180
        ? "bg-yellow-500"
        : "bg-red-500";

  const label =
    idleSeconds < 60
      ? "Active"
      : idleSeconds < 180
        ? "Idle"
        : `Idle ${Math.floor(idleSeconds / 60)}m`;

  return (
    <div class="flex items-center gap-1.5">
      <div class={`w-2 h-2 rounded-full ${color} animate-pulse`} />
      <span class="text-xs text-gray-500">{label}</span>
    </div>
  );
}
