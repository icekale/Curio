import { Save, Tags } from "lucide-react";
import { Card } from "../components/Card";

export function ClassificationPage({
  value,
  setValue,
  onSave,
  busy,
}: {
  value: string;
  setValue: (value: string) => void;
  onSave: () => void;
  busy: boolean;
}) {
  return (
    <div className="editorLayout">
      <Card title="分类 YAML" eyebrow="Rule Editor" className="editorCard">
        <div className="editorToolbar">
          <span>{value.split(/\r?\n/).length} 行</span>
          <button className="primaryButton" onClick={onSave} disabled={busy} type="button">
            <Save size={17} />
            <span>保存规则</span>
          </button>
        </div>
        <textarea
          className="yamlEditor"
          value={value}
          spellCheck={false}
          onChange={(event) => setValue(event.target.value)}
        />
      </Card>
      <Card title="规则说明" eyebrow="Guide" className="sideGuide">
        <div className="guideList">
          <Tags size={22} />
          <p>分类规则会影响后续归档路径。保存后，新扫描文件会按最新 YAML 规则匹配分类。</p>
          <p>建议先小批量扫描验证分类结果，再进行大规模整理。</p>
        </div>
      </Card>
    </div>
  );
}
