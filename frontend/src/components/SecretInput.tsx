import { Eye, EyeOff } from "lucide-react";

export function SecretInput({
  value,
  placeholder,
  visible,
  disabled,
  onChange,
  onToggle,
}: {
  value: string;
  placeholder?: string;
  visible: boolean;
  disabled?: boolean;
  onChange: (value: string) => void;
  onToggle: () => void;
}) {
  const Icon = visible ? EyeOff : Eye;
  return (
    <div className="secretInput">
      <input
        type={visible ? "text" : "password"}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
      />
      <button
        className="secretToggle"
        onClick={onToggle}
        title={visible ? "隐藏" : "显示"}
        aria-label={visible ? "隐藏" : "显示"}
        disabled={disabled}
        type="button"
      >
        <Icon size={17} />
      </button>
    </div>
  );
}
