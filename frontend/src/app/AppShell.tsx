import { AnimatePresence, motion } from "framer-motion";
import type { ReactNode } from "react";
import type { CurioConsole } from "../hooks/useCurioConsole";
import { Sidebar } from "./Sidebar";
import { CommandBar } from "./CommandBar";

export function AppShell({
  console,
  children,
}: {
  console: CurioConsole;
  children: ReactNode;
}) {
  return (
    <div className="appShell">
      <Sidebar
        page={console.page}
        setPage={console.setPage}
        stats={console.stats}
        active={Boolean(console.activeTask)}
      />
      <div className="workspace">
        <CommandBar
          page={console.page}
          activeTask={console.activeTask}
          taskProgress={console.taskProgress}
          busy={console.busy}
          refreshing={console.refreshing}
          onRefresh={console.refreshData}
          onStart={console.startScan}
          onCloudStart={console.startCloudDriveScan}
          onStop={console.stopActiveTask}
        />
        <AnimatePresence mode="wait">
          <motion.main
            className={`page page-${console.page}`}
            key={console.page}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
          >
            {children}
          </motion.main>
        </AnimatePresence>
      </div>
    </div>
  );
}
