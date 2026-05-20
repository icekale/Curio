import { AppShell } from "./app/AppShell";
import { RearchiveModal } from "./components/RearchiveModal";
import { ToastHost } from "./components/ToastHost";
import { useCurioConsole } from "./hooks/useCurioConsole";
import { AuthScreen } from "./pages/AuthScreen";
import { ClassificationPage } from "./pages/ClassificationPage";
import { CollectionsPage } from "./pages/CollectionsPage";
import { DashboardPage } from "./pages/DashboardPage";
import { LogsPage } from "./pages/LogsPage";
import { MediaPage } from "./pages/MediaPage";
import { ScanPage } from "./pages/ScanPage";
import { TemplatesPage } from "./pages/TemplatesPage";
import { TVShowsPage } from "./pages/TVShowsPage";
import { SettingsPage } from "./pages/settings/SettingsPage";

export default function App() {
  const console = useCurioConsole();

  if (!console.authChecked) {
    return (
      <>
        <AuthScreen
          loading
          token={console.authTokenDraft}
          setToken={console.setAuthTokenDraft}
          onSubmit={console.loginWithToken}
          busy={console.busy}
        />
        <ToastHost toast={console.toast} />
      </>
    );
  }

  if (console.authRequired) {
    return (
      <>
        <AuthScreen
          token={console.authTokenDraft}
          setToken={console.setAuthTokenDraft}
          onSubmit={console.loginWithToken}
          busy={console.busy}
        />
        <ToastHost toast={console.toast} />
      </>
    );
  }

  return (
    <AppShell console={console}>
      {console.page === "dashboard" && (
        <DashboardPage
          stats={console.stats}
          batches={console.batches}
          health={console.health}
          mediaFiles={console.mediaPage.items}
          activeTask={console.activeTask}
        />
      )}
      {console.page === "scan" && <ScanPage console={console} />}
      {console.page === "processing" && (
        <MediaPage console={console} mode="processing" />
      )}
      {console.page === "staging" && <MediaPage console={console} mode="staging" />}
      {console.page === "failed" && <MediaPage console={console} mode="failed" />}
      {console.page === "tv" && (
        <TVShowsPage
          page={console.tvShowPage}
          query={console.tvQuery}
          setQuery={console.setTVQuery}
          offset={console.tvOffset}
          setOffset={console.setTVOffset}
        />
      )}
      {console.page === "collections" && (
        <CollectionsPage
          page={console.collectionPage}
          query={console.collectionQuery}
          setQuery={console.setCollectionQuery}
          statusFilter={console.collectionStatus}
          setStatusFilter={console.setCollectionStatus}
          offset={console.collectionOffset}
          setOffset={console.setCollectionOffset}
          onRepairComplete={console.repairCompleteCollections}
          onRefreshCurated={console.refreshDoubanTop250}
          busy={console.busy}
        />
      )}
      {console.page === "logs" && (
        <LogsPage
          page={console.logPage}
          filter={console.logFilter}
          setFilter={console.setLogFilter}
          offset={console.logOffset}
          setOffset={console.setLogOffset}
          loading={console.logLoading}
        />
      )}
      {console.page === "classification" && (
        <ClassificationPage
          value={console.classification}
          setValue={console.setClassification}
          onSave={console.saveClassification}
          busy={console.busy}
        />
      )}
      {console.page === "templates" && (
        <TemplatesPage
          templates={console.templates}
          preview={console.preview}
          busy={console.busy}
          setTemplates={console.setTemplates}
          saveTemplate={console.saveTemplate}
          showPreview={console.showTemplatePreview}
          showToast={console.showToast}
        />
      )}
      {console.page === "settings" && <SettingsPage console={console} />}
      <RearchiveModal
        files={console.rearchiveTargets}
        draft={console.rearchiveDraft}
        busy={console.busy}
        setDraft={console.setRearchiveDraft}
        onClose={console.closeRearchive}
        onSubmit={console.submitRearchive}
      />
      <ToastHost toast={console.toast} />
    </AppShell>
  );
}
