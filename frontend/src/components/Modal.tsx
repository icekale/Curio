import { AnimatePresence, motion } from "framer-motion";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect } from "react";
import { createPortal } from "react-dom";

export function Modal({
  open,
  eyebrow,
  title,
  children,
  footer,
  onClose,
  className = "",
}: {
  open: boolean;
  eyebrow?: string;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  className?: string;
}) {
  useEffect(() => {
    if (!open || typeof window === "undefined") return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.key !== "Escape") return;
      event.preventDefault();
      onClose();
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose, open]);

  if (typeof document === "undefined") return null;

  return createPortal(
    <AnimatePresence>
      {open && (
        <motion.div
          className="modalOverlay"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
        >
          <motion.section
            className={`modalPanel ${className}`}
            role="dialog"
            aria-modal="true"
            initial={{ opacity: 0, y: 18, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.98 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
          >
            <header className="modalHeader">
              <div>
                {eyebrow && <span>{eyebrow}</span>}
                <h2>{title}</h2>
              </div>
              <button
                className="iconButton"
                onClick={onClose}
                title="关闭"
                type="button"
              >
                <X size={18} />
              </button>
            </header>
            <div className="modalBody">{children}</div>
            {footer && <footer className="modalFooter">{footer}</footer>}
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}
