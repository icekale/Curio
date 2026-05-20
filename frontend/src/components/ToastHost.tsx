import { AnimatePresence, motion } from "framer-motion";
import { Activity, CheckCircle2, XCircle } from "lucide-react";
import { createPortal } from "react-dom";
import type { ToastState } from "../hooks/useCurioConsole";

export function ToastHost({ toast }: { toast: ToastState | null }) {
  const Icon =
    toast?.tone === "error"
      ? XCircle
      : toast?.tone === "success"
        ? CheckCircle2
        : Activity;
  return createPortal(
    <div className="toastStack" aria-live="polite" aria-atomic="true">
      <AnimatePresence>
        {toast && (
          <motion.div
            className={`toast toast-${toast.tone}`}
            key={toast.id}
            initial={{ opacity: 0, x: 16, y: -8, scale: 0.98 }}
            animate={{ opacity: 1, x: 0, y: 0, scale: 1 }}
            exit={{ opacity: 0, x: 16, y: -8, scale: 0.98 }}
            transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
          >
            <Icon size={18} />
            <span>{toast.message}</span>
          </motion.div>
        )}
      </AnimatePresence>
    </div>,
    document.body,
  );
}
