export function Blackhole({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 400 400" fill="none" className={className} aria-hidden="true">
      <defs>
        <radialGradient id="bh-core" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="oklch(0.95 0.02 260)" />
          <stop offset="100%" stopColor="oklch(0.12 0.02 260)" />
        </radialGradient>
        <radialGradient id="bh-glow" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="oklch(0.75 0.14 230)" stopOpacity="0.55" />
          <stop offset="55%" stopColor="oklch(0.6 0.12 260)" stopOpacity="0.18" />
          <stop offset="100%" stopColor="transparent" stopOpacity="0" />
        </radialGradient>
      </defs>
      <circle cx="200" cy="200" r="150" fill="url(#bh-glow)" />
      <circle cx="200" cy="200" r="88" fill="url(#bh-core)" />
      {[118, 128, 139, 151].map((r, i) => (
        <circle
          key={r}
          cx="200"
          cy="200"
          r={r}
          stroke="oklch(0.82 0.1 230)"
          strokeOpacity={0.9 - i * 0.2}
          strokeWidth={i === 0 ? 2.5 : 1}
        />
      ))}
      <circle cx="200" cy="200" r="164" stroke="oklch(0.6 0.12 260)" strokeOpacity="0.28" strokeWidth="1" strokeDasharray="3 7" />
      <circle cx="200" cy="200" r="178" stroke="oklch(0.6 0.12 260)" strokeOpacity="0.14" strokeWidth="1" strokeDasharray="1 9" />
      <circle cx="200" cy="88" r="3" fill="oklch(0.9 0.1 230)" />
      <circle cx="306" cy="236" r="2" fill="oklch(0.85 0.12 300)" />
      <circle cx="120" cy="302" r="2" fill="oklch(0.88 0.1 200)" />
    </svg>
  );
}
