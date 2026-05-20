import type { ReactNode } from "react";

type CardProps = {
  title?: string;
  eyebrow?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  variant?: "surface" | "hero" | "danger" | "compact";
};

export function Card({
  title,
  eyebrow,
  action,
  children,
  className = "",
  variant = "surface",
}: CardProps) {
  const classes = ["card", `card-${variant}`, className].filter(Boolean).join(" ");
  return (
    <section className={classes}>
      {(title || action) && (
        <header className="cardHeader">
          <div>
            {eyebrow && <span className="eyebrow">{eyebrow}</span>}
            {title && <h2>{title}</h2>}
          </div>
          {action}
        </header>
      )}
      {children}
    </section>
  );
}

type MetricCardProps = {
  label: string;
  value: number | string;
  hint?: string;
  tone?: "neutral" | "success" | "warning" | "danger" | "info";
};

export function MetricCard({
  label,
  value,
  hint,
  tone = "neutral",
}: MetricCardProps) {
  return (
    <article className={`metricCard metric-${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
      {hint && <small>{hint}</small>}
    </article>
  );
}
