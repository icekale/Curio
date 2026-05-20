package p115

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"curio/internal/models"
)

func TestPlayURLForLinkNameUsesReadableChineseRouteWithoutToken(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-1", "http://localhost:8080", "电影/敦刻尔克 (2017) - 2160p UHD HEVC DTS-HD MA.iso")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(playURL, "token=") {
		t.Fatalf("expected token-free play url, got %q", playURL)
	}
	if !strings.Contains(playURL, "电影/敦刻尔克") {
		t.Fatalf("expected readable chinese route, got %q", playURL)
	}
	if strings.Contains(playURL, "id/link-1") {
		t.Fatalf("expected readable route instead of id route, got %q", playURL)
	}
	if strings.Contains(playURL, " ") {
		t.Fatalf("expected spaces to be escaped for player compatibility, got %q", playURL)
	}
	if playURL != "http://localhost:8080/play/115/电影/敦刻尔克%20(2017)%20-%202160p%20UHD%20HEVC%20DTS-HD%20MA.iso" {
		t.Fatalf("unexpected readable route %q", playURL)
	}
}

func TestPlayURLForLinkNameFallsBackToIDRoute(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-2", "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	if playURL != "http://localhost:8080/play/115/id/link-2/link-2" {
		t.Fatalf("unexpected fallback play url %q", playURL)
	}
}

func TestLinkIDFromPlayRouteAllowsDisplaySuffix(t *testing.T) {
	if got := linkIDFromPlayRoute("id/link-3/link-3.mkv"); got != "link-3" {
		t.Fatalf("unexpected link id %q", got)
	}
}

func TestPlayBaseURLIgnoresEmbyPublicURL(t *testing.T) {
	got := playBaseURL(models.P115Settings{EmbyPublicURL: "http://192.168.10.83:8097"}, "http://localhost:8080")
	if got != "http://localhost:8080" {
		t.Fatalf("unexpected play base url %q", got)
	}
}

func TestDirectURLTTLUpgradesLegacyDefault(t *testing.T) {
	got := directURLTTL(models.P115Settings{DirectURLTTLSeconds: legacyDirectURLTTLSeconds})
	if got != 50*time.Minute {
		t.Fatalf("unexpected ttl %s", got)
	}
}

func TestPreviewSamplesTakesFromEachLibraryBeforeFilling(t *testing.T) {
	groups := []previewLibraryItems{
		{Lib: LibraryConfig{CID: "cid-a"}, Items: []TreeItem{{RelativePath: "a1.mkv"}, {RelativePath: "a2.mkv"}, {RelativePath: "a3.mkv"}}},
		{Lib: LibraryConfig{CID: "cid-b"}, Items: []TreeItem{{RelativePath: "b1.mkv"}}},
		{Lib: LibraryConfig{CID: "cid-c"}, Items: []TreeItem{{RelativePath: "c1.mkv"}, {RelativePath: "c2.mkv"}}},
	}

	samples := previewSamples(groups, 5)
	got := make([]string, 0, len(samples))
	for _, sample := range samples {
		got = append(got, groups[sample.GroupIndex].Lib.CID+":"+groups[sample.GroupIndex].Items[sample.ItemIndex].RelativePath)
	}
	want := []string{"cid-a:a1.mkv", "cid-b:b1.mkv", "cid-c:c1.mkv", "cid-a:a2.mkv", "cid-c:c2.mkv"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected samples %#v", got)
	}
}

func TestOpenTokenNeedsRefreshUsesLastRefreshTime(t *testing.T) {
	recent := time.Now().Add(-openTokenRefreshInterval / 2)
	old := time.Now().Add(-openTokenRefreshInterval - time.Minute)

	if openTokenNeedsRefresh(models.P115Settings{}) {
		t.Fatal("settings without refresh token must not refresh")
	}
	if !openTokenNeedsRefresh(models.P115Settings{RefreshToken: "refresh"}) {
		t.Fatal("missing access token should refresh when refresh token exists")
	}
	if !openTokenNeedsRefresh(models.P115Settings{AccessToken: "access", RefreshToken: "refresh"}) {
		t.Fatal("missing refreshed_at should refresh")
	}
	if openTokenNeedsRefresh(models.P115Settings{AccessToken: "access", RefreshToken: "refresh", OpenTokenRefreshedAt: &recent}) {
		t.Fatal("recently refreshed token should not refresh again")
	}
	if !openTokenNeedsRefresh(models.P115Settings{AccessToken: "access", RefreshToken: "refresh", OpenTokenRefreshedAt: &old}) {
		t.Fatal("old token should refresh even when access token still exists")
	}
}

func TestWriteSTRMSkipsUnchangedContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "movie.strm")

	wrote, err := writeSTRM(root, target, "http://localhost/play")
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("expected first write")
	}

	wrote, err = writeSTRM(root, target, "http://localhost/play")
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("expected unchanged STRM to be skipped")
	}
}

func TestLocalSTRMFilesFindsOnlySTRMUnderOutputPrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "media", "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "media", "movies", "A.strm")
	if err := os.WriteFile(want, []byte("http://localhost/play\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "media", "movies", "A.nfo"), []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := localSTRMFiles(root, "media")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[cleanPathKey(want)] != want {
		t.Fatalf("unexpected STRM files %#v", files)
	}
}

func TestSTRMPathsKeepExportRootDirectory(t *testing.T) {
	settings := models.P115Settings{STRMOutputPath: t.TempDir()}
	item := TreeItem{RelativePath: "日韩电影/小森林/小森林冬春篇.iso", Name: "小森林冬春篇.iso"}

	target, err := strmPathFor(settings.STRMOutputPath, "", item.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(target), "/日韩电影/小森林/小森林冬春篇.strm") {
		t.Fatalf("path should include export root directory, got %q", target)
	}
}

func TestEnsureTreeItemsRootDirectoryPrefixesSelectedCIDName(t *testing.T) {
	items := []TreeItem{
		{RelativePath: "tv/数码宝贝 (1999)/Season 02/E01.mkv", Name: "E01.mkv"},
	}

	got := ensureTreeItemsRootDirectory(items, "media")

	if got[0].RelativePath != "media/tv/数码宝贝 (1999)/Season 02/E01.mkv" {
		t.Fatalf("expected selected root directory in preview path, got %#v", got)
	}
}

func TestEnsureTreeItemsRootDirectoryKeepsExistingSelectedCIDName(t *testing.T) {
	items := []TreeItem{
		{RelativePath: "media/tv/数码宝贝 (1999)/Season 02/E01.mkv", Name: "E01.mkv"},
	}

	got := ensureTreeItemsRootDirectory(items, "media")

	if got[0].RelativePath != items[0].RelativePath {
		t.Fatalf("expected existing root directory to be preserved, got %#v", got)
	}
}

func TestPrepareTreeItemsDeduplicatesRelativePath(t *testing.T) {
	items := []TreeItem{
		{RelativePath: "媒体库/JAV/A/A-fanart.jpg", Name: "A-fanart.jpg"},
		{RelativePath: "媒体库/JAV/A/A-fanart.jpg", Name: "A-fanart.jpg"},
		{RelativePath: "媒体库/JAV/A/A.mp4", Name: "A.mp4"},
	}

	prepared, snapshot := prepareTreeItems(LibraryConfig{CID: "cid-1"}, items, "v1")

	if len(prepared) != 2 || len(snapshot) != 2 {
		t.Fatalf("expected duplicate relative path to be skipped, got prepared=%d snapshot=%d", len(prepared), len(snapshot))
	}
	if prepared[0].RelativePath != "媒体库/JAV/A/A-fanart.jpg" || prepared[1].RelativePath != "媒体库/JAV/A/A.mp4" {
		t.Fatalf("unexpected prepared items %#v", prepared)
	}
}

func TestApplyLifeEventsUpdatesNodeTree(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	nodes := []models.P115Node{
		{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "Anime", Name: "Anime", IsDirectory: true, IsAlive: true},
		{LibraryCID: "root", RemoteFileID: "file-1", ParentFileID: "dir-1", RelativePath: "Anime/A.mkv", Name: "A.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10, IsAlive: true, IsMedia: true},
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, []LifeEvent{
		{ID: 1, Type: 20, FileID: "dir-1", ParentID: "root", Name: "Anime Renamed"},
		{ID: 2, Type: 2, FileID: "file-2", ParentID: "dir-1", Name: "B.mkv", PickCode: "pc2", SHA1: "sha2", Size: 20},
	}, "v2")
	if !changed {
		t.Fatal("expected node tree to change")
	}
	items := treeItemsFromNodes(aliveNodes(updated))
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.RelativePath] = true
	}
	for _, want := range []string{"Anime Renamed", "Anime Renamed/A.mkv", "Anime Renamed/B.mkv"} {
		if !paths[want] {
			t.Fatalf("expected path %q in %#v", want, paths)
		}
	}
}

func TestApplyLifeEventsDeletesDirectoryDescendants(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	nodes := []models.P115Node{
		{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "Anime", Name: "Anime", IsDirectory: true, IsAlive: true},
		{LibraryCID: "root", RemoteFileID: "file-1", ParentFileID: "dir-1", RelativePath: "Anime/A.mkv", Name: "A.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10, IsAlive: true, IsMedia: true},
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, []LifeEvent{{ID: 1, Type: 22, FileID: "dir-1"}}, "v2")
	if !changed {
		t.Fatal("expected delete event to change nodes")
	}
	if got := len(aliveNodes(updated)); got != 0 {
		t.Fatalf("expected no alive nodes, got %d: %#v", got, updated)
	}
}

func TestDirectoryMoveEventNeedsSubtreeScan(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "tv"}}
	if !eventsNeedTreeScan(events) {
		t.Fatal("expected directory-like move event to trigger subtree scan")
	}
	if !eventCreatesDirectory(events[0]) {
		t.Fatal("expected directory-like move event to create a directory node")
	}
}

func TestMoveFileFolderNameWithMetadataStillLooksDirectory(t *testing.T) {
	event := LifeEvent{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "雷神（系列）", PickCode: "pc-dir", SHA1: "sha-dir", Size: 4}
	if !eventCreatesDirectory(event) {
		t.Fatal("expected folder-like move_file event with metadata to be treated as directory candidate")
	}
	if !eventsNeedParentDirectoryReconcile(nil, []LifeEvent{event}) {
		t.Fatal("expected folder-like move_file event to trigger parent directory reconcile")
	}
}

func TestNewMovedDirectoryEventNeedsLibraryReconcile(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "独立日（系列）"}}
	if !eventsNeedParentDirectoryReconcile(nil, events) {
		t.Fatal("expected new moved directory to trigger parent directory reconcile")
	}
	nodes := []models.P115Node{{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "独立日（系列）", Name: "独立日（系列）", IsDirectory: true, IsAlive: true}}
	if eventsNeedParentDirectoryReconcile(nodes, events) {
		t.Fatal("expected known moved directory to stay on subtree/path incremental path")
	}
}

func TestReceiveFilesDirectoryEventNeedsLibraryReconcile(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 14, FileID: "dir-1", ParentID: "root", Name: "环太平洋（系列）"}}
	if !eventCreatesDirectory(events[0]) {
		t.Fatal("expected receive_files folder-like event to be treated as directory")
	}
	if !eventsNeedParentDirectoryReconcile(nil, events) {
		t.Fatal("expected receive_files folder-like event to trigger parent directory reconcile")
	}
}

func TestEventsNeedTreeScanKeepsNormalVideoEventIncremental(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "file-1", ParentID: "root", Name: "甜心格格 - S05E01.mp4"}}
	if eventsNeedTreeScan(events) {
		t.Fatal("expected normal video event to stay incremental")
	}
}

func TestEventsNeedTreeScanKeepsSubtitleEventFileLike(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "sub-1", ParentID: "root", Name: "甜心格格 - S05E01.ass"}}
	if eventsNeedTreeScan(events) {
		t.Fatal("expected subtitle move event to stay file-like")
	}
}

func TestAdvanceCursorWithBatchUsesRawHighWater(t *testing.T) {
	cursor := models.P115EventCursor{LibraryCID: "root", LastEventID: 10, LastEventTime: 1000}
	next := advanceCursorWithBatch(cursor, LifeEventBatch{LastEventID: 20, LastEventTime: 2000})

	if next.LastEventID != 20 || next.LastEventTime != 2000 {
		t.Fatalf("expected raw event cursor to advance, got %#v", next)
	}
}

func TestPathOnlyExportDoesNotLookLikeAuthoritativeNodes(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	items := []TreeItem{
		{RelativePath: "电影/示例.mkv", Name: "示例.mkv"},
		{RelativePath: "电影", Name: "电影", IsDirectory: true},
	}

	if treeItemsHaveRemoteIdentity(items) {
		t.Fatal("path-only exported tree must not replace the node cache")
	}
	if nodes := nodesFromTreeItems(lib, items, "v1"); len(nodes) != 0 {
		t.Fatalf("expected no authoritative nodes, got %#v", nodes)
	}
}

func TestRemoteIdentityTreeCanReplaceNodes(t *testing.T) {
	items := []TreeItem{{RelativePath: "电影/示例.mkv", Name: "示例.mkv", RemoteFileID: "file-1", ParentFileID: "dir-1", PickCode: "pc1"}}

	if !treeItemsHaveRemoteIdentity(items) {
		t.Fatal("tree with remote file ids should be authoritative for node cache")
	}
}

func TestCanMarkMissingOnlyForAuthoritativeSources(t *testing.T) {
	for _, mode := range []string{"export"} {
		if !canMarkMissingFromSource(mode) {
			t.Fatalf("expected %s to allow missing STRM marking", mode)
		}
	}
	for _, mode := range []string{"events", "events_parent_scan", "scan", "rebuild_nodes", "cache", "snapshot", "", "sync"} {
		if canMarkMissingFromSource(mode) {
			t.Fatalf("expected %s to preserve existing STRM links", mode)
		}
	}
}

func TestMovedDirectorySubtreeScanAddsChildren(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "独立日（系列）"}}
	updated, changed := applyLifeEventsToNodes(lib, nil, events, "v2")
	if !changed {
		t.Fatal("expected directory event to create root node")
	}
	roots := eventSubtreeScanRoots(updated, events)
	if len(roots) != 1 || roots[0].RemoteFileID != "dir-1" {
		t.Fatalf("expected one subtree scan root, got %#v", roots)
	}
	merged, changed := mergeScannedSubtree(lib, updated, roots[0], []TreeItem{
		{RelativePath: "独立日（系列）/独立日.mkv", Name: "独立日.mkv", RemoteFileID: "file-1", ParentFileID: "dir-1", PickCode: "pc1", SHA1: "sha1", Size: 10},
	}, "v2")
	if !changed {
		t.Fatal("expected scanned child to change node cache")
	}
	items := treeItemsFromNodes(aliveNodes(merged))
	paths := make(map[string]TreeItem, len(items))
	for _, item := range items {
		paths[item.RelativePath] = item
	}
	if !paths["独立日（系列）"].IsDirectory {
		t.Fatalf("expected moved root directory in %#v", items)
	}
	if !isMediaTreeItem(paths["独立日（系列）/独立日.mkv"]) {
		t.Fatalf("expected scanned media child in %#v", items)
	}
}

func TestParentDirectoryReconcileReplacesSyntheticFolderID(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	event := LifeEvent{ID: 1, Type: 6, FileID: "event-dir", ParentID: "root", Name: "雷神（系列）"}
	nodes, changed := applyLifeEventsToNodes(lib, nil, []LifeEvent{event}, "v2")
	if !changed {
		t.Fatal("expected synthetic event directory")
	}
	actualRoot := models.P115Node{
		LibraryCID:   "root",
		TreeVersion:  "v2",
		RemoteFileID: "actual-dir",
		ParentFileID: "root",
		RelativePath: "雷神（系列）",
		Name:         "雷神（系列）",
		IsDirectory:  true,
		IsAlive:      true,
	}
	nodes, changed = markSupersededEventDirectory(nodes, event, "v2")
	if !changed {
		t.Fatal("expected synthetic event directory to be marked dead")
	}
	nodes, changed = mergeScannedSubtree(lib, nodes, actualRoot, []TreeItem{
		{RelativePath: "雷神（系列）/雷神.mkv", Name: "雷神.mkv", RemoteFileID: "file-1", ParentFileID: "actual-dir", PickCode: "pc1", SHA1: "sha1", Size: 10},
	}, "v2")
	if !changed {
		t.Fatal("expected actual scanned directory to be merged")
	}
	items := treeItemsFromNodes(aliveNodes(nodes))
	paths := make(map[string]TreeItem, len(items))
	for _, item := range items {
		paths[item.RelativePath] = item
		if item.RemoteFileID == "event-dir" {
			t.Fatalf("synthetic event directory should not stay alive: %#v", items)
		}
	}
	if !paths["雷神（系列）"].IsDirectory || paths["雷神（系列）"].RemoteFileID != "actual-dir" {
		t.Fatalf("expected actual root directory, got %#v", items)
	}
	if !isMediaTreeItem(paths["雷神（系列）/雷神.mkv"]) {
		t.Fatalf("expected scanned child media, got %#v", items)
	}
}

func TestMergeScannedSubtreeMarksMissingDescendantsDead(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	nodes := []models.P115Node{
		{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "tv", Name: "tv", IsDirectory: true, IsAlive: true},
		{LibraryCID: "root", RemoteFileID: "file-old", ParentFileID: "dir-1", RelativePath: "tv/old.mkv", Name: "old.mkv", PickCode: "old", SHA1: "oldsha", Size: 10, IsAlive: true, IsMedia: true},
	}
	merged, changed := mergeScannedSubtree(lib, nodes, nodes[0], []TreeItem{
		{RelativePath: "tv/new.mkv", Name: "new.mkv", RemoteFileID: "file-new", ParentFileID: "dir-1", PickCode: "new", SHA1: "newsha", Size: 20},
	}, "v2")
	if !changed {
		t.Fatal("expected subtree merge to change node cache")
	}
	alive := treeItemsFromNodes(aliveNodes(merged))
	paths := make(map[string]bool, len(alive))
	for _, item := range alive {
		paths[item.RelativePath] = true
	}
	if paths["tv/old.mkv"] {
		t.Fatalf("expected old descendant to be marked dead, got %#v", alive)
	}
	if !paths["tv/new.mkv"] {
		t.Fatalf("expected new descendant, got %#v", alive)
	}
}

func TestApplyLifeEventsCreatesMovedDirectoryBeforeChildren(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	updated, changed := applyLifeEventsToNodes(lib, nil, []LifeEvent{
		{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "神兵小将 (2007)"},
		{ID: 2, Type: 6, FileID: "file-1", ParentID: "dir-1", Name: "神兵小将 - S01E52.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10},
	}, "v2")
	if !changed {
		t.Fatal("expected node tree to change")
	}
	items := treeItemsFromNodes(aliveNodes(updated))
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.RelativePath] = item.IsDirectory
	}
	if !paths["神兵小将 (2007)"] {
		t.Fatalf("expected moved directory in %#v", items)
	}
	if _, ok := paths["神兵小将 (2007)/神兵小将 - S01E52.mkv"]; !ok {
		t.Fatalf("expected moved child file in %#v", items)
	}
}
