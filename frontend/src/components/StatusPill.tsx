import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  HelpCircle,
  Info,
  XCircle,
} from "lucide-react";
import { statusLabel, statusTone, type StatusTone } from "../utils/labels";

const toneIcons = {
  success: CheckCircle2,
  running: Activity,
  warning: AlertTriangle,
  danger: XCircle,
  info: Info,
  neutral: HelpCircle,
} satisfies Record<StatusTone, typeof Activity>;

export function StatusPill({
  value,
  label,
  tone,
}: {
  value: string;
  label?: string;
  tone?: StatusTone;
}) {
  const resolvedTone = tone ?? statusTone(value);
  const Icon = toneIcons[resolvedTone];
  return (
    <span className={`statusPill status-${resolvedTone}`}>
      <Icon size={14} />
      {label ?? statusLabel(value)}
    </span>
  );
}

export function PartState({
  tone,
  children,
}: {
  tone: "success" | "danger" | "neutral";
  children: string;
}) {
  return <span className={`partState part-${tone}`}>{children}</span>;
}
