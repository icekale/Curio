import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { Copy, Info, Save, SlidersHorizontal, X } from "lucide-react";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import type { NamingTemplate } from "../types";
import type { ToastState } from "../hooks/useCurioConsole";
import {
  copyText,
  fieldDeleteRange,
  templateFieldDocs,
  templateLabels,
} from "../utils/templates";

export function TemplatesPage({
  templates,
  preview,
  busy,
  setTemplates,
  saveTemplate,
  showPreview,
  showToast,
}: {
  templates: NamingTemplate[];
  preview: string;
  busy: boolean;
  setTemplates: (value: NamingTemplate[]) => void;
  saveTemplate: (template: NamingTemplate) => void;
  showPreview: (template: NamingTemplate) => void;
  showToast: (message: string, tone?: ToastState["tone"]) => void;
}) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const [activeType, setActiveType] = useState(templates[0]?.template_type ?? "");
  const [fieldGuideOpen, setFieldGuideOpen] = useState(false);
  const activeTemplate =
    templates.find((item) => item.template_type === activeType) ?? templates[0];

  useEffect(() => {
    if (!activeType && templates[0]) setActiveType(templates[0].template_type);
  }, [activeType, templates]);

  const updateTemplate = (templateType: string, value: string) => {
    setTemplates(
      templates.map((item) =>
        item.template_type === templateType ? { ...item, template: value } : item,
      ),
    );
  };

  const insertField = (field: string) => {
    if (!activeTemplate) return;
    const textarea = textareaRef.current;
    const start = textarea?.selectionStart ?? activeTemplate.template.length;
    const end = textarea?.selectionEnd ?? start;
    const value = `${activeTemplate.template.slice(0, start)}${field}${activeTemplate.template.slice(end)}`;
    updateTemplate(activeTemplate.template_type, value);
    window.requestAnimationFrame(() => {
      textarea?.focus();
      textarea?.setSelectionRange(start + field.length, start + field.length);
    });
  };

  const deleteWholeField = (
    event: KeyboardEvent<HTMLTextAreaElement>,
    template: NamingTemplate,
  ) => {
    if (event.key !== "Backspace" && event.key !== "Delete") return;
    const textarea = event.currentTarget;
    if (textarea.selectionStart !== textarea.selectionEnd) return;
    const range = fieldDeleteRange(
      textarea.value,
      textarea.selectionStart,
      event.key === "Backspace" ? "backward" : "forward",
    );
    if (!range) return;
    event.preventDefault();
    const value = `${textarea.value.slice(0, range.start)}${textarea.value.slice(range.end)}`;
    updateTemplate(template.template_type, value);
    window.requestAnimationFrame(() => {
      textarea.focus();
      textarea.setSelectionRange(range.start, range.start);
    });
  };

  const copyField = async (field: string) => {
    try {
      await copyText(field);
      showToast(`${field} 已复制`, "success");
    } catch {
      showToast("字段复制失败", "error");
    }
  };

  return (
    <section className="templateLayout">
      <Card title="命名模板" eyebrow="Template Studio" className="templateEditor">
        <div className="templateTabs">
          {templates.map((template) => (
            <button
              key={template.template_type}
              className={activeTemplate?.template_type === template.template_type ? "active" : ""}
              onClick={() => setActiveType(template.template_type)}
              type="button"
            >
              {templateLabels[template.template_type] ?? template.template_type}
            </button>
          ))}
        </div>
        {activeTemplate ? (
          <>
            <label className="field templateField">
              <span>{templateLabels[activeTemplate.template_type] ?? activeTemplate.template_type}</span>
              <textarea
                ref={textareaRef}
                value={activeTemplate.template}
                onKeyDown={(event) => deleteWholeField(event, activeTemplate)}
                onChange={(event) =>
                  updateTemplate(activeTemplate.template_type, event.target.value)
                }
              />
            </label>
            <div className="editorActions">
              <button
                className="secondaryButton"
                onClick={() => showPreview(activeTemplate)}
                type="button"
              >
                <SlidersHorizontal size={17} />
                <span>生成预览</span>
              </button>
              <button
                className="primaryButton"
                onClick={() => saveTemplate(activeTemplate)}
                disabled={busy}
                type="button"
              >
                <Save size={17} />
                <span>保存模板</span>
              </button>
            </div>
          </>
        ) : (
          <div className="emptyState">暂无模板</div>
        )}
      </Card>

      <Card
        title="字段抽屉"
        eyebrow="Tokens"
        className="fieldDrawer"
        action={
          <button
            className="iconButton"
            onClick={() => setFieldGuideOpen(true)}
            title="字段说明"
            type="button"
          >
            <Info size={18} />
          </button>
        }
      >
        <div className="fieldGroups">
          {["基础", "技术", "剧集", "合集"].map((group) => (
            <section key={group}>
              <h3>{group}</h3>
              <div className="tokenList">
                {templateFieldDocs
                  .filter((item) => item.group === group)
                  .map((item) => (
                    <button
                      className="tokenChip"
                      key={item.field}
                      onClick={() => insertField(item.field)}
                      type="button"
                    >
                      {item.field}
                    </button>
                  ))}
              </div>
            </section>
          ))}
        </div>
      </Card>

      <Card title="预览" eyebrow="Preview" className="previewCard">
        <pre>{preview}</pre>
      </Card>

      <FieldGuide
        open={fieldGuideOpen}
        onClose={() => setFieldGuideOpen(false)}
        onCopy={copyField}
      />
    </section>
  );
}

function FieldGuide({
  open,
  onClose,
  onCopy,
}: {
  open: boolean;
  onClose: () => void;
  onCopy: (field: string) => void;
}) {
  return (
    <Modal
      open={open}
      title="字段说明"
      className="fieldGuideModal"
      onClose={onClose}
      footer={
        <button className="secondaryButton" onClick={onClose} type="button">
          <X size={17} />
          <span>关闭</span>
        </button>
      }
    >
      <div className="fieldGuideList">
        {templateFieldDocs.map((item) => (
          <button
            className="fieldGuideItem"
            key={item.field}
            onClick={() => onCopy(item.field)}
            type="button"
          >
            <code>{item.field}</code>
            <span>
              <b>{item.name}</b>
              <small>{item.description}</small>
            </span>
            <Copy size={17} />
          </button>
        ))}
      </div>
    </Modal>
  );
}
