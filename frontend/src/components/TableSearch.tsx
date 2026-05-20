import { Search } from "lucide-react";

export function TableSearch({
  value,
  onChange,
  placeholder = "模糊搜索",
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="tableSearch">
      <Search size={16} />
      <input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}
