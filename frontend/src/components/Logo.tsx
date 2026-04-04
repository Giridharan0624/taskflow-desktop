import logoSrc from "../assets/logo.png";

/**
 * TaskFlow logo — uses the actual logo image.
 */
export function TaskFlowLogo({ size = 36, class: className }: { size?: number; class?: string }) {
  return (
    <img
      src={logoSrc}
      alt="TaskFlow"
      class={className}
      style={{
        width: size,
        height: size,
        borderRadius: "22%",
      }}
    />
  );
}
