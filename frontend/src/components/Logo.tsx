/**
 * TaskFlow logo — matches the web app's Logo component.
 * Gradient rounded square with TF monogram SVG, visually centered.
 */
export function TaskFlowLogo({ size = 36, class: className }: { size?: number; class?: string }) {
  return (
    <div
      class={className}
      style={{
        width: size,
        height: size,
        borderRadius: "22%",
        background: "linear-gradient(135deg, #4f46e5, #6366f1, #7c3aed)",
        boxShadow: "0 4px 12px rgba(99, 102, 241, 0.25)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <svg viewBox="0 0 32 32" fill="none" style={{ width: "62%", height: "62%" }}>
        {/* Vertical stem — shifted right to center */}
        <path d="M13 6v20" stroke="white" stroke-width="3.2" stroke-linecap="round" />
        {/* Top crossbar with arrow */}
        <path d="M7 6h14l4 4" stroke="white" stroke-width="2.8" stroke-linecap="round" stroke-linejoin="round" />
        {/* Middle bar — flow arrow */}
        <path d="M13 16h8l3 3" stroke="rgba(255,255,255,0.55)" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </div>
  );
}
