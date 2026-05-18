package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"curio/internal/aifilename"
	"curio/internal/classifier"
	"curio/internal/clouddrive"
	"curio/internal/collection"
	"curio/internal/mediainfo"
	"curio/internal/models"
	"curio/internal/naming"
	"curio/internal/organizer"
	"curio/internal/parser"
	"curio/internal/repository"
	"curio/internal/scanner"
	"curio/internal/scraper"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	store   *repository.Store
	scraper *scraper.Client
	checker *collection.Checker
	redis   *redis.Client
	mu      sync.Mutex
	active  map[string]*activeTask
}

type activeTask struct {
	batchID string
	source  string
	cancel  context.CancelFunc
	unlock  func()
}

type RearchiveOptions struct {
	TMDBID        int
	MediaType     string
	Season        int
	Episode       int
	SeasonOffset  int
	EpisodeOffset int
	SourcePath    string
	TargetRoot    string
}

type RearchiveFailure struct {
	FileID  string `json:"file_id"`
	Message string `json:"message"`
}

var ErrTaskAlreadyRunning = errors.New("已有扫描任务正在运行")
var ErrTaskNotFound = errors.New("没有正在运行的任务")
var ErrCloudDriveNotReady = errors.New("CloudDrive2 未就绪")

const scanFileConcurrency = 2

var (
	chsSubtitleRE     = regexp.MustCompile(`(?i)(^|[ ._\-\[\]()])(?:chs|sc|zh[-_]?cn|zh[-_]?sg|zh[-_]?hans|hans|gb2312|gbk|简体|简中)(?:$|[ ._\-\[\]()])`)
	chtSubtitleRE     = regexp.MustCompile(`(?i)(^|[ ._\-\[\]()])(?:cht|tc|zh[-_]?tw|zh[-_]?hk|zh[-_]?hant|hant|big5|繁体|繁中)(?:$|[ ._\-\[\]()])`)
	chsSubtitleTokens = map[string]struct{}{
		"chs": {}, "sc": {}, "hans": {}, "gb": {}, "gbk": {}, "gb2312": {}, "gb18030": {},
		"zhcn": {}, "zhsg": {}, "zhhans": {}, "jpsc": {}, "jpchs": {}, "jpschs": {},
		"scjp": {}, "chsjp": {}, "jphans": {}, "simplified": {},
	}
	chtSubtitleTokens = map[string]struct{}{
		"cht": {}, "tc": {}, "hant": {}, "big5": {},
		"zhtw": {}, "zhhk": {}, "zhmo": {}, "zhhant": {}, "jptc": {}, "jpcht": {}, "jptcht": {},
		"tcjp": {}, "chtjp": {}, "jphant": {}, "traditional": {},
	}
)

func New(store *repository.Store, scraperClient *scraper.Client, checker *collection.Checker, redisClient *redis.Client) *Service {
	return &Service{store: store, scraper: scraperClient, checker: checker, redis: redisClient, active: map[string]*activeTask{}}
}

func (s *Service) StartScan(ctx context.Context) (string, error) {
	dirs, err := s.store.Directories(ctx)
	if err != nil {
		return "", err
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return "", err
	}
	batchID, runCtx, err := s.start(ctx, models.BatchSourceLocal)
	if err != nil {
		return "", err
	}
	go s.runBatch(runCtx, batchID, dirs, settings)
	return batchID, nil
}

func (s *Service) StartCloudDriveScan(ctx context.Context) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	if err := s.checkCloudDrive(ctx, settings); err != nil {
		return "", fmt.Errorf("%w：%v", ErrCloudDriveNotReady, err)
	}
	systemSettings, err := s.store.Settings(ctx)
	if err != nil {
		return "", err
	}
	batchID, runCtx, err := s.start(ctx, models.BatchSourceCloud)
	if err != nil {
		return "", err
	}
	go s.runCloudDriveBatch(runCtx, batchID, settings, systemSettings)
	return batchID, nil
}

func (s *Service) start(ctx context.Context, source string) (string, context.Context, error) {
	s.mu.Lock()
	if len(s.active) > 0 {
		s.mu.Unlock()
		return "", nil, ErrTaskAlreadyRunning
	}
	s.mu.Unlock()
	if _, ok, err := s.store.ActiveBatch(ctx); err != nil {
		return "", nil, err
	} else if ok {
		return "", nil, ErrTaskAlreadyRunning
	}
	locked, unlockGlobal, err := s.acquireLock(ctx, "lock:scan:active", 24*time.Hour)
	if err != nil {
		return "", nil, err
	}
	if !locked {
		return "", nil, ErrTaskAlreadyRunning
	}
	locked, unlockSource, err := s.acquireLock(ctx, "lock:scan:"+source, 24*time.Hour)
	if err != nil {
		unlockGlobal()
		return "", nil, err
	}
	if !locked {
		unlockGlobal()
		return "", nil, ErrTaskAlreadyRunning
	}
	unlock := func() {
		unlockSource()
		unlockGlobal()
	}
	batchID := uuid.NewString()
	runCtx, cancel := context.WithCancel(context.Background())
	if err := s.store.CreateBatch(ctx, batchID, source); err != nil {
		cancel()
		unlock()
		return "", nil, err
	}
	s.mu.Lock()
	if len(s.active) > 0 {
		s.mu.Unlock()
		cancel()
		unlock()
		_ = s.store.FinishBatch(ctx, batchID, models.BatchStatusCancelled)
		return "", nil, ErrTaskAlreadyRunning
	}
	s.active[batchID] = &activeTask{batchID: batchID, source: source, cancel: cancel, unlock: unlock}
	s.mu.Unlock()
	return batchID, runCtx, nil
}

func (s *Service) ActiveBatch(ctx context.Context) (models.Batch, bool, error) {
	s.mu.Lock()
	for batchID := range s.active {
		s.mu.Unlock()
		batch, err := s.store.Batch(ctx, batchID)
		return batch, err == nil, err
	}
	s.mu.Unlock()
	return s.store.ActiveBatch(ctx)
}

func (s *Service) Stop(ctx context.Context, batchID string) (models.Batch, error) {
	s.mu.Lock()
	if batchID == "" && len(s.active) == 1 {
		for id := range s.active {
			batchID = id
		}
	}
	task := s.active[batchID]
	s.mu.Unlock()
	if batchID == "" {
		return models.Batch{}, ErrTaskNotFound
	}
	if task != nil {
		_ = s.store.SetBatchStatus(ctx, batchID, models.BatchStatusCancelling)
		task.cancel()
		return s.store.Batch(ctx, batchID)
	}
	batch, err := s.store.Batch(ctx, batchID)
	if err != nil {
		return models.Batch{}, err
	}
	if isActiveBatchStatus(batch.Status) {
		if err := s.store.FinishBatch(ctx, batchID, models.BatchStatusInterrupted); err != nil {
			return models.Batch{}, err
		}
		return s.store.Batch(ctx, batchID)
	}
	return batch, nil
}

func (s *Service) RearchiveByTMDBID(ctx context.Context, fileID string, tmdbID int) (models.MediaFile, error) {
	return s.RearchiveMedia(ctx, fileID, RearchiveOptions{TMDBID: tmdbID})
}

func (s *Service) RearchiveMediaBatch(ctx context.Context, fileIDs []string, options RearchiveOptions) ([]models.MediaFile, []RearchiveFailure, error) {
	files := make([]models.MediaFile, 0, len(fileIDs))
	failures := make([]RearchiveFailure, 0)
	for _, fileID := range fileIDs {
		if err := ctx.Err(); err != nil {
			return files, failures, err
		}
		file, err := s.RearchiveMedia(ctx, fileID, options)
		if err != nil {
			failures = append(failures, RearchiveFailure{FileID: fileID, Message: err.Error()})
			continue
		}
		files = append(files, file)
	}
	return files, failures, nil
}

func (s *Service) RearchiveMedia(ctx context.Context, fileID string, options RearchiveOptions) (models.MediaFile, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return models.MediaFile{}, errors.New("媒体文件 ID 不能为空")
	}
	if options.TMDBID < 0 {
		return models.MediaFile{}, errors.New("TMDB ID 不能小于 0")
	}
	file, err := s.store.MediaFile(ctx, fileID)
	if err != nil {
		return models.MediaFile{}, err
	}
	if file.ProcessStatus != models.StatusFailed && file.ProcessStatus != models.StatusDone && file.ProcessStatus != models.StatusIncompleteCollection {
		return models.MediaFile{}, errors.New("仅支持失败或已归档记录重新归档")
	}
	sourcePath := rearchiveSourcePath(file, options.SourcePath)
	if sourcePath == "" {
		return models.MediaFile{}, errors.New("媒体文件缺少可移动路径")
	}
	file.CurrentPath = sourcePath
	parsed, err := rearchiveParsed(file, options)
	if err != nil {
		return models.MediaFile{}, err
	}
	if file.Extension == "" {
		file.Extension = parsed.Extension
	}
	dirs, err := s.rearchiveDirs(ctx, file)
	if err != nil {
		return models.MediaFile{}, err
	}
	if err := applyRearchiveTargetRoot(&dirs, file, options.TargetRoot); err != nil {
		return models.MediaFile{}, err
	}
	systemSettings, err := s.store.Settings(ctx)
	if err != nil {
		return models.MediaFile{}, err
	}
	var drive *clouddrive.DriveSession
	if clouddrive.IsURI(file.CurrentPath) {
		settings, err := s.store.CloudDriveSettings(ctx)
		if err != nil {
			return models.MediaFile{}, err
		}
		drive, err = clouddrive.New(settings).Open(ctx)
		if err != nil {
			return models.MediaFile{}, err
		}
		defer drive.Close()
	}
	result, err := s.scraper.Scrape(ctx, parsed)
	if err != nil {
		return models.MediaFile{}, err
	}
	if err := s.persistMetadata(ctx, result); err != nil {
		return models.MediaFile{}, err
	}
	technical := mediaTechnical(file)
	templateType := result.MediaType
	root := dirs.StagingPath
	finalStatus := models.StatusDone
	values := namingValues(parsed, technical, result)
	category, err := classifier.Match(systemSettings.ClassificationYAML, result.MediaType, classifierItem(result))
	if err != nil {
		return models.MediaFile{}, err
	}
	if category != "" {
		values["category"] = category
	}
	if result.MediaType == models.MediaCollectionMovie {
		check, err := s.checker.Check(ctx, result.Collection.TMDBID, result.Movie.TMDBID)
		if err != nil {
			return models.MediaFile{}, err
		}
		status := "incomplete"
		if check.Complete {
			status = "complete"
		}
		_ = s.store.UpdateCollectionStatus(ctx, result.Collection.TMDBID, check.LocalCount, status)
		if !check.Complete {
			templateType = models.TemplateIncompleteCollection
			root = dirs.IncompleteCollectionsPath
			finalStatus = models.StatusIncompleteCollection
		} else {
			templateType = models.TemplateCollectionMovie
		}
	}
	template, err := s.store.Template(ctx, templateType)
	if err != nil {
		return models.MediaFile{}, err
	}
	renderTemplate := templateWithImplicitCategory(template.Template, values)
	technical, err = s.ensureTechnicalForTemplate(ctx, file, technical, templateType, renderTemplate, drive)
	if err != nil {
		return models.MediaFile{}, err
	}
	values = namingValues(parsed, technical, result)
	if category != "" {
		values["category"] = category
	}
	renderTemplate = templateWithImplicitCategory(template.Template, values)
	_, targetPath, err := naming.Render(templateType, renderTemplate, values, root)
	if err != nil {
		return models.MediaFile{}, err
	}
	cloudSource := clouddrive.IsURI(file.CurrentPath)
	sourceDir := filepath.Dir(file.CurrentPath)
	if cloudSource {
		sourceDir = path.Dir(clouddrive.FromURI(file.CurrentPath))
	}
	sidecars := s.findRearchiveSidecars(ctx, file, cloudSource, drive)
	if !cloudSource {
		if _, err := os.Stat(targetPath); err == nil && filepath.Clean(file.CurrentPath) != filepath.Clean(targetPath) {
			return models.MediaFile{}, errors.New(models.ErrTargetPathExists)
		}
	}
	_ = s.store.IncrementMoveAttempt(ctx, file.ID)
	movedPath := targetPath
	if cloudSource {
		movedPath, err = drive.MoveFile(ctx, file.CurrentPath, targetPath)
	} else {
		err = organizer.MoveFileContext(ctx, file.CurrentPath, targetPath)
	}
	if err != nil {
		return models.MediaFile{}, err
	}
	finalName := filepath.Base(targetPath)
	if cloudSource {
		finalName = path.Base(clouddrive.FromURI(movedPath))
	}
	if err := s.persistMatch(ctx, file.ID, result); err != nil {
		return models.MediaFile{}, err
	}
	parseTitle, parseYear := parsedDisplay(parsed)
	for attempt := 0; attempt < 3; attempt++ {
		err = s.store.UpdateMediaRearchiveFinal(ctx, file.ID, parseTitle, parseYear, parsed.Season, parsed.Episode, parsed.Source, technical, result.MediaType, targetPath, movedPath, finalName, finalStatus)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	if err != nil {
		return models.MediaFile{}, err
	}
	_ = s.store.AddHistory(ctx, uuid.NewString(), "", file.ID, file.BatchID, file.CurrentPath, movedPath, "manual_rearchive", finalStatus, "", "")
	if err := s.moveSidecars(ctx, file, "", sidecars, targetPath, cloudSource, drive); err != nil {
		_ = s.store.AddHistory(ctx, uuid.NewString(), "", file.ID, file.BatchID, file.CurrentPath, targetPath, "subtitle_move", models.StatusFailed, models.ErrSubtitleMoveFailed, err.Error())
	}
	s.cleanupEmptyParents(ctx, dirs, sourceDir, sidecars, cloudSource, drive)
	_ = s.store.RecountBatch(ctx, file.BatchID)
	_ = s.store.RefreshCollectionLocalCounts(ctx)
	s.reconcileCompleteCollection(ctx, dirs, result, values, cloudSource)
	return s.store.MediaFile(ctx, file.ID)
}

func (s *Service) Recover(ctx context.Context) error {
	if err := s.store.InterruptActiveBatches(ctx); err != nil {
		return err
	}
	if s.redis == nil {
		return nil
	}
	return s.redis.Del(ctx,
		"queue:scan",
		"queue:parse",
		"queue:scrape",
		"queue:match",
		"queue:collection_check",
		"queue:organize",
		"queue:failed",
		"lock:scan:active",
		"lock:scan:"+models.BatchSourceLocal,
		"lock:scan:"+models.BatchSourceCloud,
	).Err()
}

func (s *Service) RepairCompleteCollections(ctx context.Context) (int, error) {
	if err := s.store.RefreshCollectionLocalCounts(ctx); err != nil {
		return 0, err
	}
	candidates, err := s.store.CompleteCollectionRepairCandidates(ctx)
	if err != nil {
		return 0, err
	}
	repaired := 0
	var firstErr error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return repaired, err
		}
		if len(candidate.FilePaths) == 0 {
			continue
		}
		if err := s.repairCompleteCollection(ctx, candidate); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		repaired++
	}
	return repaired, firstErr
}

func (s *Service) repairCompleteCollection(ctx context.Context, candidate repository.CollectionRepairCandidate) error {
	cloudSource := clouddrive.IsURI(candidate.FilePaths[0])
	var dirs models.DirectoryConfig
	var err error
	if cloudSource {
		settings, settingsErr := s.store.CloudDriveSettings(ctx)
		if settingsErr != nil {
			return settingsErr
		}
		dirs = cloudDriveDirs(settings)
	} else {
		dirs, err = s.store.Directories(ctx)
		if err != nil {
			return err
		}
	}
	sourceRoot := repairCollectionSourceRoot(candidate.FilePaths, dirs.IncompleteCollectionsPath, cloudSource)
	if strings.TrimSpace(sourceRoot) == "" {
		return nil
	}
	values := repairCollectionValues(candidate.Collection, sourceRoot, dirs.IncompleteCollectionsPath, cloudSource)
	collectionTemplate, err := s.store.Template(ctx, models.TemplateCollectionMovie)
	if err != nil {
		return err
	}
	collectionRenderTemplate := templateWithImplicitCategory(collectionTemplate.Template, values)
	targetRoot, err := naming.CollectionRoot(collectionRenderTemplate, values, dirs.StagingPath)
	if err != nil {
		return err
	}
	return s.withLock(ctx, fmt.Sprintf("lock:collection:%d", candidate.Collection.TMDBID), time.Minute, func() error {
		_ = s.store.UpdateCollectionStatus(ctx, candidate.Collection.TMDBID, candidate.Collection.LocalCount, "complete")
		if cloudSource {
			return s.moveCompleteCloudCollection(ctx, candidate.Collection, sourceRoot, targetRoot)
		}
		return s.moveCompleteCollection(ctx, candidate.Collection, sourceRoot, targetRoot)
	})
}

func (s *Service) runBatch(ctx context.Context, batchID string, dirs models.DirectoryConfig, settings models.SystemSettings) {
	defer s.finishActive(batchID)
	err := s.runSafely(batchID, func() error {
		return s.withQueue(ctx, "queue:scan", batchID, func() error {
			if err := s.store.SetBatchStatus(ctx, batchID, models.BatchStatusRunning); err != nil && ctx.Err() == nil {
				return err
			}
			files, err := scanner.Scan(ctx, dirs.IncomingPath)
			if err != nil {
				return err
			}
			_ = s.store.SetBatchTotal(ctx, batchID, len(files))
			_ = s.progress(ctx, batchID)
			if err := s.processFiles(ctx, batchID, dirs, files, settings, nil); err != nil {
				return err
			}
			return s.progress(ctx, batchID)
		})
	})
	s.finishBatch(ctx, batchID, err)
}

func (s *Service) runCloudDriveBatch(ctx context.Context, batchID string, settings models.CloudDriveSettings, systemSettings models.SystemSettings) {
	dirs := cloudDriveDirs(settings)
	defer s.finishActive(batchID)
	err := s.runSafely(batchID, func() error {
		return s.withQueue(ctx, "queue:scan", batchID, func() error {
			if err := s.store.SetBatchStatus(ctx, batchID, models.BatchStatusRunning); err != nil && ctx.Err() == nil {
				return err
			}
			drive, err := clouddrive.New(settings).Open(ctx)
			if err != nil {
				return err
			}
			defer drive.Close()
			files, err := drive.Scan(ctx, settings.RootPath, settings.StagingPath, settings.FailedPath, settings.IncompleteCollectionsPath)
			if err != nil {
				return err
			}
			_ = s.store.SetBatchTotal(ctx, batchID, len(files))
			_ = s.progress(ctx, batchID)
			if err := s.processFiles(ctx, batchID, dirs, files, systemSettings, drive); err != nil {
				return err
			}
			return s.progress(ctx, batchID)
		})
	})
	s.finishBatch(ctx, batchID, err)
}

func (s *Service) processFiles(ctx context.Context, batchID string, dirs models.DirectoryConfig, files []scanner.File, settings models.SystemSettings, drive *clouddrive.DriveSession) error {
	if len(files) == 0 {
		return nil
	}
	forcedAI, err := s.preAnalyzeFilenames(ctx, batchID, files, settings)
	if err != nil {
		return err
	}
	workers := scanFileConcurrency
	if len(files) < workers {
		workers = len(files)
	}
	jobs := make(chan scanner.File)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for scanned := range jobs {
				if err := ctx.Err(); err != nil {
					sendFirstErr(errs, err)
					return
				}
				analysis, hasAnalysis := forcedAI[scanned.Path]
				if err := s.processFile(ctx, batchID, dirs, scanned, settings, analysis, hasAnalysis, drive); isContextStopped(ctx, err) {
					sendFirstErr(errs, err)
					return
				}
			}
		}()
	}
	for _, scanned := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- scanned:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

func sendFirstErr(errs chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case errs <- err:
	default:
	}
}

const aiFilenameBatchSize = 20

func (s *Service) preAnalyzeFilenames(ctx context.Context, batchID string, files []scanner.File, settings models.SystemSettings) (map[string]aifilename.Analysis, error) {
	results := map[string]aifilename.Analysis{}
	if !settings.AIFilenameEnabled || !settings.AIFilenameForce {
		return results, nil
	}
	client, err := aifilename.New(aiFilenameSettings(settings))
	if err != nil {
		return nil, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: err.Error()}
	}
	pending := make([]aifilename.File, 0, min(aiFilenameBatchSize, len(files)))
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		result, err := client.AnalyzeDetailed(ctx, pending)
		if err != nil {
			for _, file := range pending {
				s.recordAIFilenameLog(ctx, batchID, "", "force_batch", file, result.Log, nil, "failed", err.Error())
			}
			return err
		}
		byIndex := make(map[int]aifilename.Analysis, len(result.Items))
		for _, analysis := range result.Items {
			byIndex[analysis.Index] = analysis
		}
		for _, file := range pending {
			analysis, ok := byIndex[file.Index]
			if ok {
				results[file.Path] = analysis
				s.recordAIFilenameLog(ctx, batchID, "", "force_batch", file, result.Log, &analysis, "success", "")
			} else {
				s.recordAIFilenameLog(ctx, batchID, "", "force_batch", file, result.Log, nil, "failed", "AI 文件名识别没有返回该文件结果")
			}
		}
		pending = pending[:0]
		return nil
	}
	for index, file := range files {
		if file.ErrorCode != "" {
			continue
		}
		item := aiFilenameFile(index, file)
		pending = append(pending, item)
		if len(pending) >= aiFilenameBatchSize {
			if err := flush(); err != nil {
				return nil, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: "AI 文件名识别失败: " + err.Error()}
			}
		}
	}
	if err := flush(); err != nil {
		return nil, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: "AI 文件名识别失败: " + err.Error()}
	}
	return results, nil
}

func (s *Service) parseWithAI(ctx context.Context, batchID, fileID string, scanned scanner.File, settings models.SystemSettings, forcedAnalysis aifilename.Analysis, hasForcedAnalysis bool) (parser.Result, error) {
	if settings.AIFilenameEnabled && settings.AIFilenameForce {
		if hasForcedAnalysis {
			return parsedFromAI(scanned, forcedAnalysis)
		}
		return s.parseSingleWithAI(ctx, batchID, fileID, scanned, settings, nil, "force_single")
	}
	parsed, err := parser.ParsePath(scanned.Path)
	if err != nil {
		if settings.AIFilenameEnabled {
			if aiParsed, aiErr := s.parseSingleWithAI(ctx, batchID, fileID, scanned, settings, nil, "fallback_parse_error"); aiErr == nil {
				return aiParsed, nil
			} else {
				code, message := parseErr(err)
				return parser.Result{}, parser.ParseError{Code: code, Message: message + "; AI 兜底失败: " + aiErr.Error()}
			}
		}
		return parsed, err
	}
	if settings.AIFilenameEnabled && needsAIFilenameFallback(parsed) {
		if aiParsed, aiErr := s.parseSingleWithAI(ctx, batchID, fileID, scanned, settings, &parsed, "fallback_low_confidence"); aiErr == nil {
			return aiParsed, nil
		}
	}
	return parsed, nil
}

func (s *Service) parseSingleWithAI(ctx context.Context, batchID, fileID string, scanned scanner.File, settings models.SystemSettings, local *parser.Result, source string) (parser.Result, error) {
	client, err := aifilename.New(aiFilenameSettings(settings))
	if err != nil {
		return parser.Result{}, err
	}
	file := aiFilenameFile(0, scanned)
	result, err := client.AnalyzeDetailed(ctx, []aifilename.File{file})
	if err != nil {
		s.recordAIFilenameLog(ctx, batchID, fileID, source, file, result.Log, nil, "failed", err.Error())
		return parser.Result{}, err
	}
	if len(result.Items) == 0 {
		err := errors.New("AI 文件名识别没有返回结果")
		s.recordAIFilenameLog(ctx, batchID, fileID, source, file, result.Log, nil, "failed", err.Error())
		return parser.Result{}, err
	}
	analysis := result.Items[0]
	s.recordAIFilenameLog(ctx, batchID, fileID, source, file, result.Log, &analysis, "success", "")
	return parsedFromAIWithLocal(scanned, analysis, local)
}

func needsAIFilenameFallback(parsed parser.Result) bool {
	if parsed.Confidence > 0 && parsed.Confidence < 70 {
		return true
	}
	if parsed.IsTV {
		return strings.TrimSpace(parsed.ShowTitle) == "" || (parsed.Episode <= 0 && strings.TrimSpace(parsed.AirDate) == "")
	}
	return strings.TrimSpace(parsed.Title) == "" || parsed.Year == 0
}

func aiFilenameSettings(settings models.SystemSettings) aifilename.Settings {
	return aifilename.Settings{
		Enabled:      settings.AIFilenameEnabled,
		Force:        settings.AIFilenameForce,
		BaseURL:      settings.AIBaseURL,
		APIKey:       settings.AIAPIKey,
		Model:        settings.AIModel,
		Prompt:       settings.AIFilenamePrompt,
		NetworkProxy: settings.NetworkProxy,
	}
}

func aiFilenameFile(index int, file scanner.File) aifilename.File {
	return aifilename.File{
		Index:     index,
		Path:      file.Path,
		Name:      file.Name,
		Extension: file.Extension,
		Size:      file.Size,
	}
}

func (s *Service) recordAIFilenameLog(ctx context.Context, batchID, fileID, source string, file aifilename.File, call aifilename.CallLog, analysis *aifilename.Analysis, status, errorMessage string) {
	if s == nil || s.store == nil {
		return
	}
	entry := models.AIFilenameLog{
		ID:             uuid.NewString(),
		BatchID:        batchID,
		FileID:         fileID,
		FilePath:       file.Path,
		FileName:       file.Name,
		Source:         source,
		Status:         status,
		Model:          call.Model,
		BaseURL:        call.BaseURL,
		ProxyURL:       call.ProxyURL,
		ResponseFormat: call.ResponseFormat,
		RequestJSON:    call.RequestJSON,
		ResponseJSON:   call.ResponseJSON,
		HTTPStatus:     call.HTTPStatus,
		DurationMS:     call.DurationMS,
		Attempt:        call.Attempt,
		ErrorMessage:   firstString(errorMessage, call.ErrorMessage),
	}
	if entry.FileName == "" {
		entry.FileName = path.Base(strings.ReplaceAll(file.Path, "\\", "/"))
	}
	if analysis != nil {
		entry.MediaType = analysis.MediaType
		entry.Title = analysis.Title
		entry.Year = analysis.Year
		entry.Season = analysis.Season
		entry.Episode = analysis.Episode
		entry.Confidence = analysis.Confidence
		entry.NeedsReview = analysis.NeedsReview
		entry.Reason = analysis.Reason
		if raw, err := json.Marshal(analysis); err == nil {
			entry.ParsedJSON = string(raw)
		}
	}
	if entry.Status == "" {
		if entry.ErrorMessage != "" {
			entry.Status = "failed"
		} else {
			entry.Status = "success"
		}
	}
	_ = s.store.AddAIFilenameLog(ctx, entry)
}

func parsedFromAI(scanned scanner.File, analysis aifilename.Analysis) (parser.Result, error) {
	return parsedFromAIWithLocal(scanned, analysis, nil)
}

func parsedFromAIWithLocal(scanned scanner.File, analysis aifilename.Analysis, local *parser.Result) (parser.Result, error) {
	base := parser.Result{}
	if local != nil {
		base = *local
	} else if parsed, err := parser.ParsePath(scanned.Path); err == nil {
		base = parsed
	}
	base.Parser = "curio-ai"
	base.Confidence = aiConfidence(analysis.Confidence)
	if base.Confidence == 0 {
		base.Confidence = 80
	}
	base.Extension = firstString(base.Extension, strings.TrimPrefix(strings.ToLower(filepath.Ext(scanned.Name)), "."), scanned.Extension)
	base.Source = valueOrUnknown(firstString(base.Source, analysis.Source))
	base.Edition = firstString(analysis.Edition, base.Edition)
	base.ReleaseGroup = firstString(analysis.ReleaseGroup, base.ReleaseGroup)
	base.AlternativeTitles = uniqueStrings(append(base.AlternativeTitles, analysis.AlternativeTitles...)...)
	switch analysis.MediaType {
	case models.MediaMovie:
		title := strings.TrimSpace(analysis.Title)
		if title == "" {
			return parser.Result{}, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: "AI 文件名识别没有返回电影标题"}
		}
		year := analysis.Year
		if year == 0 {
			year = base.Year
		}
		base.IsTV = false
		base.Type = "movie"
		base.Title = title
		base.Year = year
		base.ShowTitle = ""
		base.ShowYear = 0
		base.Season = 0
		base.Episode = 0
		base.Season2 = ""
		base.Episode2 = ""
		base.Episodes = nil
		base.SearchTitles = uniqueStrings(append([]string{title}, analysis.AlternativeTitles...)...)
		return base, nil
	case models.MediaTVEpisode:
		title := strings.TrimSpace(analysis.Title)
		if title == "" {
			return parser.Result{}, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: "AI 文件名识别没有返回剧集标题"}
		}
		episode := analysis.Episode
		if episode <= 0 {
			return parser.Result{}, parser.ParseError{Code: models.ErrParseEpisodeEmpty, Message: "AI 文件名识别没有返回集号"}
		}
		season := analysis.Season
		if season <= 0 {
			season = 1
		}
		showYear := analysis.Year
		if showYear == 0 {
			showYear = base.ShowYear
		}
		base.IsTV = true
		base.Type = "episode"
		base.Title = ""
		base.Year = 0
		base.ShowTitle = title
		base.ShowYear = showYear
		base.Season = season
		base.Episode = episode
		base.Season2 = fmt.Sprintf("%02d", season)
		base.Episode2 = fmt.Sprintf("%02d", episode)
		base.Episodes = aiEpisodeRange(episode, analysis.EpisodeEnd)
		base.SearchTitles = uniqueStrings(append([]string{title}, analysis.AlternativeTitles...)...)
		return base, nil
	default:
		return parser.Result{}, parser.ParseError{Code: models.ErrParseTitleEmpty, Message: "AI 文件名识别无法判断媒体类型"}
	}
}

func aiConfidence(value float64) int {
	switch {
	case value <= 0:
		return 0
	case value <= 1:
		return int(value*100 + 0.5)
	case value > 100:
		return 100
	default:
		return int(value + 0.5)
	}
}

func aiEpisodeRange(start, end int) []int {
	if start <= 0 {
		return nil
	}
	if end < start || end-start > 100 {
		return []int{start}
	}
	out := make([]int, 0, end-start+1)
	for current := start; current <= end; current++ {
		out = append(out, current)
	}
	return out
}

func uniqueStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) runSafely(batchID string, fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("任务异常退出：%v", recovered)
			log.Printf("batch %s panic: %v\n%s", batchID, recovered, debug.Stack())
		}
	}()
	return fn()
}

func (s *Service) checkCloudDrive(ctx context.Context, settings models.CloudDriveSettings) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	drive, err := clouddrive.New(settings).Open(ctx)
	if err != nil {
		return fmt.Errorf("连接失败：%w%s", err, cloudDriveAddressHint(settings.Address))
	}
	defer drive.Close()
	root := clouddrive.NormalizePath(settings.RootPath)
	if _, err := drive.List(ctx, root); err != nil {
		return fmt.Errorf("读取根目录 %s 失败：%w%s", root, err, cloudDriveAddressHint(settings.Address))
	}
	return nil
}

func cloudDriveAddressHint(address string) string {
	value := strings.ToLower(strings.TrimSpace(address))
	if strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1") || strings.Contains(value, "::1") {
		return "；当前 CloudDrive2 地址指向容器自身，在 NAS Docker 中请改为 http://host.docker.internal:19798 或宿主机/NAS 的局域网地址"
	}
	return ""
}

func (s *Service) finishActive(batchID string) {
	s.mu.Lock()
	task := s.active[batchID]
	delete(s.active, batchID)
	s.mu.Unlock()
	if task != nil && task.unlock != nil {
		task.unlock()
	}
}

func (s *Service) finishBatch(runCtx context.Context, batchID string, runErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status := models.BatchStatusComplete
	if runErr != nil {
		status = models.BatchStatusFailed
		if runCtx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			status = models.BatchStatusCancelled
		}
	}
	_ = s.store.FinishBatch(ctx, batchID, status)
	_ = s.progress(ctx, batchID)
}

func isActiveBatchStatus(status string) bool {
	return status == models.BatchStatusQueued || status == models.BatchStatusRunning || status == models.BatchStatusCancelling
}

func isContextStopped(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *Service) processFile(ctx context.Context, batchID string, dirs models.DirectoryConfig, scanned scanner.File, settings models.SystemSettings, forcedAnalysis aifilename.Analysis, hasForcedAnalysis bool, drive *clouddrive.DriveSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file := models.MediaFile{
		ID:            uuid.NewString(),
		BatchID:       batchID,
		OriginalPath:  scanned.Path,
		CurrentPath:   scanned.Path,
		OriginalName:  scanned.Name,
		Extension:     scanned.Extension,
		FileSize:      scanned.Size,
		FileHash:      scanned.Hash,
		HashType:      scanned.HashType,
		ProcessStatus: models.StatusIncoming,
	}
	if err := s.store.CreateMediaFile(ctx, file); err != nil {
		_ = s.store.IncrementBatch(ctx, batchID, models.StatusFailed)
		_ = s.progress(ctx, batchID)
		return err
	}
	_ = s.store.AttachAIFilenameLogFile(ctx, batchID, scanned.Path, file.ID)
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.FileHash != "" {
		locked, unlock, err := s.acquireLock(ctx, "lock:file:"+file.FileHash, time.Hour)
		if err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		if !locked {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, "文件正在被其他任务处理")
			return nil
		}
		defer unlock()
	}
	if scanned.ErrorCode != "" {
		s.fail(ctx, dirs, file, "", scanned.ErrorCode, scanned.ErrorMessage)
		return nil
	}
	return s.withQueue(ctx, "queue:parse", file.ID, func() error {
		if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusScanned); err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		parsed, err := s.parseWithAI(ctx, batchID, file.ID, scanned, settings, forcedAnalysis, hasForcedAnalysis)
		if err != nil {
			code, message := parseErr(err)
			s.fail(ctx, dirs, file, "", code, message)
			return err
		}
		technical := unknownTechnical()
		parseTitle := parsed.Title
		parseYear := parsed.Year
		if parsed.IsTV {
			parseTitle = parsed.ShowTitle
			parseYear = parsed.ShowYear
		}
		if err := s.store.UpdateMediaParsed(ctx, file.ID, parseTitle, parseYear, parsed.Season, parsed.Episode, parsed.Source, technical); err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		return s.scrapeAndOrganize(ctx, dirs, file, parsed, technical, settings.ClassificationYAML, scanned.Sidecars, drive)
	})
}

func (s *Service) scrapeAndOrganize(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, parsed parser.Result, technical models.MediaTechnicalInfo, classificationYAML string, sidecars []scanner.Sidecar, drive *clouddrive.DriveSession) error {
	var result scraper.Result
	err := s.withQueue(ctx, "queue:scrape", file.ID, func() error {
		var scrapeErr error
		result, scrapeErr = s.scraper.Scrape(ctx, parsed)
		return scrapeErr
	})
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		code, message := scrapeErr(err)
		s.fail(ctx, dirs, file, "", code, message)
		return err
	}
	if result.MediaType == models.MediaTVEpisode {
		updated := syncParsedTVEpisode(parsed, result)
		if updated.ShowYear != parsed.ShowYear || updated.Season != parsed.Season || updated.Episode != parsed.Episode {
			if err := s.store.UpdateMediaParsedTV(ctx, file.ID, updated.ShowYear, updated.Season, updated.Episode); err != nil {
				if isContextStopped(ctx, err) {
					return err
				}
				s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
				return err
			}
		}
		parsed = updated
	}
	if err := s.persistMatch(ctx, file.ID, result); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusScraped); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.withQueue(ctx, "queue:match", file.ID, func() error {
		return s.store.UpdateMediaMatched(ctx, file.ID, result.MediaType)
	}); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	return s.planAndMove(ctx, dirs, file, parsed, technical, result, classificationYAML, sidecars, drive)
}

func (s *Service) planAndMove(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, parsed parser.Result, technical models.MediaTechnicalInfo, result scraper.Result, classificationYAML string, sidecars []scanner.Sidecar, drive *clouddrive.DriveSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cloudSource := clouddrive.IsURI(file.CurrentPath)
	templateType := result.MediaType
	root := dirs.StagingPath
	finalStatus := models.StatusDone
	values := namingValues(parsed, technical, result)
	category, err := classifier.Match(classificationYAML, result.MediaType, classifierItem(result))
	if err != nil {
		s.fail(ctx, dirs, file, "", models.ErrTemplateFieldInvalid, err.Error())
		return err
	}
	if category != "" {
		values["category"] = category
	}
	if result.MediaType == models.MediaCollectionMovie {
		var check collection.CheckResult
		err := s.withQueue(ctx, "queue:collection_check", file.ID, func() error {
			var checkErr error
			check, checkErr = s.checker.Check(ctx, result.Collection.TMDBID, result.Movie.TMDBID)
			return checkErr
		})
		if err != nil {
			if isContextStopped(ctx, err) {
				return err
			}
			s.fail(ctx, dirs, file, "", models.ErrCollectionCheckFailed, err.Error())
			return err
		}
		status := "incomplete"
		if check.Complete {
			status = "complete"
		}
		_ = s.store.UpdateCollectionStatus(ctx, result.Collection.TMDBID, check.LocalCount, status)
		if !check.Complete {
			templateType = models.TemplateIncompleteCollection
			root = dirs.IncompleteCollectionsPath
			finalStatus = models.StatusIncompleteCollection
		} else {
			templateType = models.TemplateCollectionMovie
		}
	}
	if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusCollectionChecked); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	template, err := s.store.Template(ctx, templateType)
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrTemplateNotFound, err.Error())
		return err
	}
	var targetPath string
	renderTemplate := templateWithImplicitCategory(template.Template, values)
	technical, err = s.ensureTechnicalForTemplate(ctx, file, technical, templateType, renderTemplate, drive)
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", workerErrorCode(err, models.ErrMediaProbeFailed), err.Error())
		return err
	}
	values = namingValues(parsed, technical, result)
	if category != "" {
		values["category"] = category
	}
	renderTemplate = templateWithImplicitCategory(template.Template, values)
	if err := s.withQueue(ctx, "queue:organize", file.ID, func() error {
		_, target, renderErr := naming.Render(templateType, renderTemplate, values, root)
		targetPath = target
		return renderErr
	}); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", templateErrCode(err), err.Error())
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cloudSource {
		if _, err := os.Stat(targetPath); err == nil {
			s.fail(ctx, dirs, file, "", models.ErrTargetPathExists, "目标路径已存在")
			return err
		}
	}
	taskID := uuid.NewString()
	if err := s.store.CreateTask(ctx, taskID, file.ID, file.BatchID, template.TemplateType, file.CurrentPath, targetPath, models.StatusPlanned); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, taskID, models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.store.UpdateMediaPlanned(ctx, file.ID, targetPath); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, taskID, models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sourceDir := filepath.Dir(file.CurrentPath)
	if cloudSource {
		sourceDir = path.Dir(clouddrive.FromURI(file.CurrentPath))
	}
	_ = s.store.IncrementMoveAttempt(ctx, file.ID)
	movedPath := targetPath
	var moveErr error
	if cloudSource {
		if drive != nil {
			movedPath, moveErr = drive.MoveFile(ctx, file.CurrentPath, targetPath)
		} else {
			movedPath, moveErr = s.moveCloudFile(ctx, file.CurrentPath, targetPath)
		}
	} else {
		moveErr = organizer.MoveFileContext(ctx, file.CurrentPath, targetPath)
	}
	if moveErr != nil {
		if isContextStopped(ctx, moveErr) {
			return moveErr
		}
		code := models.ErrMoveToStagingFailed
		if finalStatus == models.StatusIncompleteCollection {
			code = models.ErrMoveToIncompleteCollectionFailed
		}
		message := moveErr.Error()
		if moveErr.Error() == models.ErrTargetPathExists {
			code = models.ErrTargetPathExists
			message = "目标路径已存在"
		}
		_ = s.store.FinishTask(ctx, taskID, models.StatusFailed, code, message)
		s.fail(ctx, dirs, file, taskID, code, message)
		return moveErr
	}
	finalName := filepath.Base(targetPath)
	if cloudSource {
		finalName = path.Base(clouddrive.FromURI(movedPath))
	}
	if err := s.finishMovedMedia(ctx, file, taskID, movedPath, finalName, finalStatus, cloudSource, drive); err != nil {
		return err
	}
	_ = s.store.FinishTask(ctx, taskID, models.StatusDone, "", "")
	_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, movedPath, "move", finalStatus, "", "")
	if err := s.moveSidecars(ctx, file, taskID, sidecars, targetPath, cloudSource, drive); err != nil {
		_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, targetPath, "subtitle_move", models.StatusFailed, models.ErrSubtitleMoveFailed, err.Error())
	}
	s.cleanupEmptyParents(ctx, dirs, sourceDir, sidecars, cloudSource, drive)
	_ = s.store.IncrementBatch(ctx, file.BatchID, finalStatus)
	s.reconcileCompleteCollection(ctx, dirs, result, values, cloudSource)
	return s.progress(ctx, file.BatchID)
}

func (s *Service) persistMetadata(ctx context.Context, result scraper.Result) error {
	switch result.MediaType {
	case models.MediaMovie:
		return s.store.UpsertMovie(ctx, result.Movie)
	case models.MediaCollectionMovie:
		collection := *result.Collection
		if err := s.store.UpsertCollection(ctx, collection); err != nil {
			return err
		}
		for _, part := range collection.Parts {
			if err := s.store.UpsertCollectionMovie(ctx, part); err != nil {
				return err
			}
		}
		if err := s.store.UpsertMovie(ctx, result.Movie); err != nil {
			return err
		}
		return nil
	case models.MediaTVEpisode:
		if err := s.store.UpsertTVShow(ctx, result.TVShow); err != nil {
			return err
		}
		episodes := result.TVEpisodes
		if len(episodes) == 0 {
			episodes = []models.TVEpisodeMetadata{result.TVEpisode}
		}
		for _, episode := range episodes {
			if err := s.store.UpsertTVEpisode(ctx, episode); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("未知媒体类型 %s", result.MediaType)
	}
}

func (s *Service) persistMatch(ctx context.Context, fileID string, result scraper.Result) error {
	if err := s.persistMetadata(ctx, result); err != nil {
		return err
	}
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		return s.store.UpsertMediaMatch(ctx, fileID, result.MediaType, result.Movie.TMDBID, 0, "")
	case models.MediaTVEpisode:
		return s.store.UpsertMediaMatch(ctx, fileID, result.MediaType, 0, result.TVShow.TMDBID, result.TVEpisode.ID)
	default:
		return fmt.Errorf("未知媒体类型 %s", result.MediaType)
	}
}

func (s *Service) reconcileCompleteCollection(ctx context.Context, dirs models.DirectoryConfig, result scraper.Result, values map[string]string, cloudSource bool) {
	if result.MediaType != models.MediaCollectionMovie || result.Collection == nil {
		return
	}
	_ = s.withLock(ctx, fmt.Sprintf("lock:collection:%d", result.Collection.TMDBID), time.Minute, func() error {
		check, err := s.checker.Check(ctx, result.Collection.TMDBID, result.Movie.TMDBID)
		if err != nil {
			return err
		}
		status := "incomplete"
		if check.Complete {
			status = "complete"
		}
		_ = s.store.UpdateCollectionStatus(ctx, result.Collection.TMDBID, check.LocalCount, status)
		if !check.Complete {
			return nil
		}
		if cloudSource {
			return s.migrateCompleteCloudCollectionLocked(ctx, dirs, result, values)
		}
		return s.migrateCompleteCollectionLocked(ctx, dirs, result, values)
	})
}

func (s *Service) migrateCompleteCollectionLocked(ctx context.Context, dirs models.DirectoryConfig, result scraper.Result, values map[string]string) error {
	collectionTemplate, err := s.store.Template(ctx, models.TemplateCollectionMovie)
	if err != nil {
		return err
	}
	incompleteTemplate, err := s.store.Template(ctx, models.TemplateIncompleteCollection)
	if err != nil {
		return err
	}
	incompleteRenderTemplate := templateWithImplicitCategory(incompleteTemplate.Template, values)
	collectionRenderTemplate := templateWithImplicitCategory(collectionTemplate.Template, values)
	sourceRoot, err := naming.TemplateRoot(models.TemplateIncompleteCollection, incompleteRenderTemplate, values, dirs.IncompleteCollectionsPath)
	if err != nil {
		return err
	}
	targetRoot, err := naming.CollectionRoot(collectionRenderTemplate, values, dirs.StagingPath)
	if err != nil {
		return err
	}
	return s.moveCompleteCollection(ctx, *result.Collection, sourceRoot, targetRoot)
}

func (s *Service) moveCompleteCollection(ctx context.Context, collection models.CollectionMetadata, sourceRoot, targetRoot string) error {
	if err := organizer.MigrateCollection(sourceRoot, targetRoot); err != nil {
		_ = s.store.AddHistory(ctx, uuid.NewString(), "", "", "", sourceRoot, targetRoot, "collection_complete_move", models.StatusFailed, models.ErrCollectionCompleteMoveFailed, err.Error())
		return err
	}
	batchIDs, err := s.store.UpdateCollectionPathPrefix(ctx, collection.TMDBID, sourceRoot, targetRoot)
	if err != nil {
		return err
	}
	for _, batchID := range batchIDs {
		_ = s.store.RecountBatch(ctx, batchID)
	}
	if len(batchIDs) > 0 {
		_ = s.store.AddCollectionCompletionHistory(ctx, uuid.NewString(), collection.TMDBID, collection.Name, sourceRoot, targetRoot, collection.MovieCount)
	}
	return nil
}

func (s *Service) fail(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, taskID, code, message string) {
	if ctx.Err() != nil {
		return
	}
	var failedPath string
	archiveErr := s.withQueue(ctx, "queue:failed", file.ID, func() error {
		var err error
		if clouddrive.IsURI(file.CurrentPath) {
			failedPath, err = s.archiveCloudFailure(ctx, dirs, file, code, message)
		} else {
			failedPath, err = organizer.ArchiveFailure(ctx, dirs, file, code, message)
		}
		return err
	})
	finalCode := code
	finalMessage := message
	currentPath := failedPath
	if archiveErr != nil {
		finalCode = models.ErrMoveToFailedDirFailed
		finalMessage = fmt.Sprintf("%s; original %s: %s", archiveErr.Error(), code, message)
		currentPath = file.CurrentPath
	} else if !clouddrive.IsURI(file.CurrentPath) {
		organizer.RemoveEmptyParents(filepath.Dir(file.CurrentPath), dirs.IncomingPath)
	}
	_ = s.store.UpdateMediaFailure(ctx, file.ID, currentPath, failedPath, finalCode, finalMessage)
	_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, failedPath, "failed_archive", models.StatusFailed, finalCode, finalMessage)
	_ = s.store.IncrementBatch(ctx, file.BatchID, models.StatusFailed)
	_ = s.progress(ctx, file.BatchID)
}

func (s *Service) moveCloudFile(ctx context.Context, sourceURI, targetPath string) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	return clouddrive.New(settings).MoveFile(ctx, sourceURI, targetPath)
}

func (s *Service) archiveCloudFailure(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, code, message string) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	return clouddrive.New(settings).ArchiveFailure(ctx, dirs.FailedPath, file, code, message)
}

func (s *Service) probeTechnical(ctx context.Context, file models.MediaFile, drive *clouddrive.DriveSession) (models.MediaTechnicalInfo, error) {
	source := mediainfo.Source{
		Path:      file.CurrentPath,
		Extension: file.Extension,
		Size:      file.FileSize,
	}
	if clouddrive.IsURI(file.CurrentPath) {
		if drive == nil {
			settings, err := s.store.CloudDriveSettings(ctx)
			if err != nil {
				return models.MediaTechnicalInfo{}, err
			}
			opened, err := clouddrive.New(settings).Open(ctx)
			if err != nil {
				return models.MediaTechnicalInfo{}, err
			}
			defer opened.Close()
			drive = opened
		}
		mode := clouddrive.ProbeDownloadMode()
		if strings.EqualFold(file.Extension, "iso") && mode == clouddrive.DownloadModeProxy && clouddrive.ProbePrefetchEnabled() {
			_ = drive.Prefetch(ctx, file.CurrentPath, []clouddrive.ByteRange{{Start: 0, Length: 16 * 1024 * 1024}})
		}
		download, err := drive.DownloadURLWithMode(ctx, file.CurrentPath, mode)
		if err != nil {
			return models.MediaTechnicalInfo{}, err
		}
		if download.Mode == string(clouddrive.DownloadModeProxy) {
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = drive.CloseFileReader(closeCtx, file.CurrentPath)
			}()
		}
		source.Path = ""
		source.URL = mediainfo.CleanURL(download.URL)
		source.Headers = download.Headers
		source.UserAgent = download.UserAgent
	}
	info, err := mediainfo.Probe(ctx, source)
	if err != nil {
		return models.MediaTechnicalInfo{}, err
	}
	return models.MediaTechnicalInfo{
		Resolution:    info.Resolution,
		VideoCodec:    info.VideoCodec,
		AudioCodec:    info.AudioCodec,
		AudioChannels: info.AudioChannels,
		HDRFormat:     info.HDRFormat,
	}, nil
}

func (s *Service) ensureTechnicalForTemplate(ctx context.Context, file models.MediaFile, technical models.MediaTechnicalInfo, templateType, template string, drive *clouddrive.DriveSession) (models.MediaTechnicalInfo, error) {
	if _, err := naming.Fields(templateType, template); err != nil {
		return technical, codedWorkerError{code: models.ErrTemplateFieldInvalid, err: err}
	}
	probed, err := s.probeTechnical(ctx, file, drive)
	if err != nil {
		return technical, codedWorkerError{code: models.ErrMediaProbeFailed, err: err}
	}
	if err := s.store.UpdateMediaTechnical(ctx, file.ID, probed); err != nil {
		return probed, codedWorkerError{code: models.ErrDatabaseWriteFailed, err: err}
	}
	return probed, nil
}

func unknownTechnical() models.MediaTechnicalInfo {
	return models.MediaTechnicalInfo{
		Resolution:    mediainfo.Unknown,
		VideoCodec:    mediainfo.Unknown,
		AudioCodec:    mediainfo.Unknown,
		AudioChannels: mediainfo.Unknown,
		HDRFormat:     mediainfo.Unknown,
	}
}

func mediaTechnical(file models.MediaFile) models.MediaTechnicalInfo {
	technical := models.MediaTechnicalInfo{
		Resolution:    file.Resolution,
		VideoCodec:    file.VideoCodec,
		AudioCodec:    file.AudioCodec,
		AudioChannels: file.AudioChannels,
		HDRFormat:     file.HDRFormat,
	}
	if technical.Resolution == "" {
		technical.Resolution = mediainfo.Unknown
	}
	if technical.VideoCodec == "" {
		technical.VideoCodec = mediainfo.Unknown
	}
	if technical.AudioCodec == "" {
		technical.AudioCodec = mediainfo.Unknown
	}
	if technical.AudioChannels == "" {
		technical.AudioChannels = mediainfo.Unknown
	}
	if technical.HDRFormat == "" {
		technical.HDRFormat = mediainfo.Unknown
	}
	return technical
}

func unknownValue(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, mediainfo.Unknown)
}

func mediaSourcePath(file models.MediaFile) string {
	for _, value := range []string{file.CurrentPath, file.FinalPath, file.LastVerified, file.OriginalPath} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func rearchiveSourcePath(file models.MediaFile, override string) string {
	override = strings.TrimSpace(override)
	if override == "" {
		return mediaSourcePath(file)
	}
	if clouddrive.IsURI(override) {
		return clouddrive.URI(clouddrive.FromURI(override))
	}
	if mediaHasCloudPath(file) && strings.HasPrefix(strings.ReplaceAll(override, "\\", "/"), "/") {
		return clouddrive.URI(override)
	}
	return override
}

func mediaHasCloudPath(file models.MediaFile) bool {
	for _, value := range []string{file.CurrentPath, file.FinalPath, file.LastVerified, file.OriginalPath} {
		if clouddrive.IsURI(value) {
			return true
		}
	}
	return false
}

func rearchiveParsed(file models.MediaFile, options RearchiveOptions) (parser.Result, error) {
	var parsed parser.Result
	var err error
	for _, value := range []string{file.OriginalPath, file.CurrentPath, file.FinalPath, file.OriginalName} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err = parser.ParsePath(value)
		if err == nil {
			break
		}
	}
	if err != nil {
		parsed = parser.Result{
			Title:      fallbackTitle(file),
			Year:       file.ParseYear,
			Source:     valueOrUnknown(file.Source),
			Extension:  file.Extension,
			Resolution: mediainfo.Unknown,
			VideoCodec: mediainfo.Unknown,
		}
	}
	options.MediaType = strings.TrimSpace(options.MediaType)
	if options.MediaType == models.MediaMovie {
		parsed.IsTV = false
		parsed.Season = 0
		parsed.Episode = 0
		parsed.Season2 = ""
		parsed.Episode2 = ""
		if parsed.Title == "" {
			parsed.Title = fallbackTitle(file)
		}
		if parsed.Year == 0 {
			parsed.Year = file.ParseYear
		}
	}
	if parsed.Extension == "" {
		parsed.Extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(file.OriginalName)), ".")
	}
	if parsed.Source == "" || strings.EqualFold(parsed.Source, mediainfo.Unknown) {
		parsed.Source = valueOrUnknown(file.Source)
	}
	forceTV := options.MediaType == models.MediaTVEpisode ||
		options.Season > 0 || options.Episode > 0 ||
		options.SeasonOffset != 0 || options.EpisodeOffset != 0
	if parsed.IsTV || forceTV || file.MediaType == models.MediaTVEpisode || file.Season > 0 || file.Episode > 0 {
		parsed.IsTV = true
		if parsed.Season == 0 {
			parsed.Season = file.Season
		}
		if parsed.Episode == 0 {
			parsed.Episode = file.Episode
		}
		parsed.Season += options.SeasonOffset
		parsed.Episode += options.EpisodeOffset
		if options.Season > 0 {
			parsed.Season = options.Season
		}
		if options.Episode > 0 {
			parsed.Episode = options.Episode
		}
		if parsed.Season < 0 || parsed.Episode <= 0 {
			return parser.Result{}, errors.New("季号不能小于 0，集号必须大于 0")
		}
		parsed.Season2 = fmt.Sprintf("%02d", parsed.Season)
		parsed.Episode2 = fmt.Sprintf("%02d", parsed.Episode)
		if parsed.ShowTitle == "" {
			parsed.ShowTitle = fallbackTitle(file)
		}
		if parsed.ShowYear == 0 {
			parsed.ShowYear = file.ParseYear
		}
		if parsed.Episode == 0 {
			return parser.Result{}, errors.New("剧集重新归档需要可识别的季号和集号")
		}
	}
	parsed.TMDBID = options.TMDBID
	return parsed, nil
}

func fallbackTitle(file models.MediaFile) string {
	if strings.TrimSpace(file.ParseTitle) != "" {
		return strings.TrimSpace(file.ParseTitle)
	}
	name := file.OriginalName
	if name == "" {
		name = filepath.Base(mediaSourcePath(file))
	}
	ext := filepath.Ext(name)
	return strings.TrimSpace(strings.TrimSuffix(name, ext))
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return mediainfo.Unknown
	}
	return strings.TrimSpace(value)
}

func syncParsedTVEpisode(parsed parser.Result, result scraper.Result) parser.Result {
	if result.MediaType != models.MediaTVEpisode {
		return parsed
	}
	parsed.IsTV = true
	if strings.TrimSpace(parsed.ShowTitle) == "" {
		parsed.ShowTitle = result.TVShow.Name
	}
	if parsed.ShowYear == 0 {
		parsed.ShowYear = result.TVShow.Year
	}
	parsed.Season = result.TVEpisode.Season
	parsed.Episode = result.TVEpisode.Episode
	parsed.Season2 = fmt.Sprintf("%02d", result.TVEpisode.Season)
	parsed.Episode2 = fmt.Sprintf("%02d", result.TVEpisode.Episode)
	if parsed.Episode > 0 {
		parsed.Episodes = []int{parsed.Episode}
	}
	if strings.TrimSpace(parsed.AirDate) == "" {
		parsed.AirDate = result.TVEpisode.AirDate
	}
	return parsed
}

func parsedDisplay(parsed parser.Result) (string, int) {
	if parsed.IsTV {
		return parsed.ShowTitle, parsed.ShowYear
	}
	return parsed.Title, parsed.Year
}

func (s *Service) rearchiveDirs(ctx context.Context, file models.MediaFile) (models.DirectoryConfig, error) {
	if clouddrive.IsURI(file.CurrentPath) {
		settings, err := s.store.CloudDriveSettings(ctx)
		if err != nil {
			return models.DirectoryConfig{}, err
		}
		return cloudDriveDirs(settings), nil
	}
	return s.store.Directories(ctx)
}

func applyRearchiveTargetRoot(dirs *models.DirectoryConfig, file models.MediaFile, targetRoot string) error {
	targetRoot = strings.TrimSpace(targetRoot)
	if targetRoot == "" {
		return nil
	}
	if clouddrive.IsURI(file.CurrentPath) {
		root := clouddrive.NormalizePath(targetRoot)
		if root == "/" {
			return errors.New("target_root cannot be /")
		}
		dirs.StagingPath = root
		return nil
	}
	if !filepath.IsAbs(targetRoot) {
		return errors.New("target_root must be an absolute path")
	}
	dirs.StagingPath = filepath.Clean(targetRoot)
	return nil
}

func (s *Service) findRearchiveSidecars(ctx context.Context, file models.MediaFile, cloudSource bool, drive *clouddrive.DriveSession) []scanner.Sidecar {
	if cloudSource {
		return findCloudRearchiveSidecars(ctx, file, drive)
	}
	return findLocalRearchiveSidecars(file)
}

func findCloudRearchiveSidecars(ctx context.Context, file models.MediaFile, drive *clouddrive.DriveSession) []scanner.Sidecar {
	if drive == nil {
		return nil
	}
	source := clouddrive.FromURI(file.CurrentPath)
	sourceDir := path.Dir(source)
	items, err := drive.List(ctx, sourceDir)
	if err != nil {
		return nil
	}
	media, sidecars, childDirs := collectCloudRearchiveItems(items)
	ensureCurrentRearchiveMedia(&media, file, true)
	for _, dir := range childDirs {
		children, err := drive.List(ctx, dir)
		if err != nil {
			continue
		}
		for _, item := range children {
			if item.IsDirectory || !scanner.IsSubtitleExtension(rearchiveExt(item.Name, item.Extension)) {
				continue
			}
			sidecars = append(sidecars, scanner.Sidecar{
				Path:      cloudItemURI(item),
				Name:      item.Name,
				Extension: strings.TrimPrefix(rearchiveExt(item.Name, item.Extension), "."),
				Size:      item.Size,
			})
		}
	}
	return selectRearchiveSidecars(media, sidecars, file.CurrentPath, true)
}

func collectCloudRearchiveItems(items []clouddrive.File) ([]scanner.File, []scanner.Sidecar, []string) {
	media := make([]scanner.File, 0)
	sidecars := make([]scanner.Sidecar, 0)
	childDirs := make([]string, 0)
	for _, item := range items {
		if item.IsDirectory {
			childDirs = append(childDirs, item.Path)
			continue
		}
		ext := rearchiveExt(item.Name, item.Extension)
		switch {
		case scanner.IsSubtitleExtension(ext):
			sidecars = append(sidecars, scanner.Sidecar{
				Path:      cloudItemURI(item),
				Name:      item.Name,
				Extension: strings.TrimPrefix(ext, "."),
				Size:      item.Size,
			})
		case scanner.IsMediaExtension(ext):
			media = append(media, scanner.File{
				Path:      cloudItemURI(item),
				Name:      item.Name,
				Extension: strings.TrimPrefix(ext, "."),
				Size:      item.Size,
				Hash:      item.Hash,
				HashType:  item.HashType,
			})
		}
	}
	return media, sidecars, childDirs
}

func findLocalRearchiveSidecars(file models.MediaFile) []scanner.Sidecar {
	sourceDir := filepath.Dir(file.CurrentPath)
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil
	}
	media := make([]scanner.File, 0)
	sidecars := make([]scanner.Sidecar, 0)
	childDirs := make([]string, 0)
	for _, entry := range entries {
		fullPath := filepath.Join(sourceDir, entry.Name())
		if entry.IsDir() {
			childDirs = append(childDirs, fullPath)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ext := rearchiveExt(entry.Name(), "")
		switch {
		case scanner.IsSubtitleExtension(ext):
			sidecars = append(sidecars, scanner.Sidecar{
				Path:      fullPath,
				Name:      entry.Name(),
				Extension: strings.TrimPrefix(ext, "."),
				Size:      info.Size(),
			})
		case scanner.IsMediaExtension(ext):
			media = append(media, scanner.File{
				Path:      fullPath,
				Name:      entry.Name(),
				Extension: strings.TrimPrefix(ext, "."),
				Size:      info.Size(),
			})
		}
	}
	ensureCurrentRearchiveMedia(&media, file, false)
	for _, dir := range childDirs {
		children, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range children {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			ext := rearchiveExt(entry.Name(), "")
			if !scanner.IsSubtitleExtension(ext) {
				continue
			}
			sidecars = append(sidecars, scanner.Sidecar{
				Path:      filepath.Join(dir, entry.Name()),
				Name:      entry.Name(),
				Extension: strings.TrimPrefix(ext, "."),
				Size:      info.Size(),
			})
		}
	}
	return selectRearchiveSidecars(media, sidecars, file.CurrentPath, false)
}

func selectRearchiveSidecars(media []scanner.File, sidecars []scanner.Sidecar, sourcePath string, cloudSource bool) []scanner.Sidecar {
	if len(sidecars) == 0 {
		return nil
	}
	attached := scanner.AttachSidecars(media, sidecars)
	sourceKey := rearchivePathKey(sourcePath, cloudSource)
	sourceDir := rearchiveDirKey(sourcePath, cloudSource)
	selected := make([]scanner.Sidecar, 0)
	seen := map[string]struct{}{}
	add := func(sidecar scanner.Sidecar) {
		key := rearchivePathKey(sidecar.Path, cloudSource)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		selected = append(selected, sidecar)
	}
	for _, item := range attached {
		if rearchivePathKey(item.Path, cloudSource) != sourceKey {
			continue
		}
		for _, sidecar := range item.Sidecars {
			add(sidecar)
		}
		break
	}
	mediaInSourceDir := 0
	for _, item := range media {
		if rearchiveDirKey(item.Path, cloudSource) == sourceDir {
			mediaInSourceDir++
		}
	}
	if mediaInSourceDir == 1 {
		for _, sidecar := range sidecars {
			if rearchiveDirKey(sidecar.Path, cloudSource) == sourceDir {
				add(sidecar)
			}
		}
	}
	return selected
}

func ensureCurrentRearchiveMedia(media *[]scanner.File, file models.MediaFile, cloudSource bool) {
	sourceKey := rearchivePathKey(file.CurrentPath, cloudSource)
	for _, item := range *media {
		if rearchivePathKey(item.Path, cloudSource) == sourceKey {
			return
		}
	}
	name := filepath.Base(file.CurrentPath)
	pathValue := file.CurrentPath
	if cloudSource {
		source := clouddrive.FromURI(file.CurrentPath)
		name = path.Base(source)
		pathValue = clouddrive.URI(source)
	}
	ext := rearchiveExt(name, file.Extension)
	*media = append(*media, scanner.File{
		Path:      pathValue,
		Name:      name,
		Extension: strings.TrimPrefix(ext, "."),
		Size:      file.FileSize,
		Hash:      file.FileHash,
		HashType:  file.HashType,
	})
}

func cloudItemURI(item clouddrive.File) string {
	if strings.TrimSpace(item.URI) != "" {
		return item.URI
	}
	return clouddrive.URI(item.Path)
}

func rearchiveExt(name, ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		ext = filepath.Ext(name)
	}
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		return ""
	}
	return "." + ext
}

func rearchivePathKey(value string, cloudSource bool) string {
	if cloudSource {
		return strings.ToLower(clouddrive.NormalizePath(clouddrive.FromURI(value)))
	}
	return strings.ToLower(filepath.Clean(value))
}

func rearchiveDirKey(value string, cloudSource bool) string {
	if cloudSource {
		return strings.ToLower(path.Dir(clouddrive.NormalizePath(clouddrive.FromURI(value))))
	}
	return strings.ToLower(filepath.Clean(filepath.Dir(value)))
}

func (s *Service) finishMovedMedia(ctx context.Context, file models.MediaFile, taskID, movedPath, finalName, finalStatus string, cloudSource bool, drive *clouddrive.DriveSession) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.store.UpdateMediaFinal(ctx, file.ID, movedPath, movedPath, finalName, finalStatus); err == nil {
			return nil
		} else if attempt == 2 {
			verified := false
			if cloudSource && drive != nil {
				verified = drive.PathExists(ctx, movedPath)
			} else if !cloudSource {
				verified = organizer.PathExists(movedPath)
			}
			message := err.Error()
			if verified {
				message = "文件已移动到目标路径，但数据库状态更新失败：" + message
				_ = s.store.UpdateMediaFailure(ctx, file.ID, movedPath, movedPath, models.ErrDatabaseWriteFailed, message)
			}
			_ = s.store.FinishTask(ctx, taskID, models.StatusFailed, models.ErrDatabaseWriteFailed, message)
			_ = s.store.IncrementBatch(ctx, file.BatchID, models.StatusFailed)
			_ = s.progress(ctx, file.BatchID)
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return nil
}

func (s *Service) moveSidecars(ctx context.Context, file models.MediaFile, taskID string, sidecars []scanner.Sidecar, mediaTarget string, cloudSource bool, drive *clouddrive.DriveSession) error {
	if len(sidecars) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, sidecar := range sidecars {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := subtitleTarget(mediaTarget, sidecar, counts)
		var err error
		var moved string
		if cloudSource {
			if drive != nil {
				moved, err = drive.MoveFile(ctx, sidecar.Path, target)
			} else {
				moved, err = s.moveCloudFile(ctx, sidecar.Path, target)
			}
		} else {
			err = organizer.MoveFileContext(ctx, sidecar.Path, target)
			moved = target
		}
		if err != nil {
			return err
		}
		_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, sidecar.Path, moved, "subtitle_move", models.StatusDone, "", "")
	}
	return nil
}

func (s *Service) cleanupEmptyParents(ctx context.Context, dirs models.DirectoryConfig, sourceDir string, sidecars []scanner.Sidecar, cloudSource bool, drive *clouddrive.DriveSession) {
	stopRoot := cleanupStopRoot(dirs, sourceDir, cloudSource)
	sidecarDirs := make([]string, 0, len(sidecars))
	seen := map[string]struct{}{}
	for _, sidecar := range sidecars {
		dir := filepath.Dir(sidecar.Path)
		if cloudSource {
			dir = path.Dir(clouddrive.FromURI(sidecar.Path))
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		sidecarDirs = append(sidecarDirs, dir)
	}
	for _, dir := range sidecarDirs {
		if cloudSource && drive != nil {
			drive.DeleteEmptyParents(ctx, dir, stopRoot)
		} else if !cloudSource {
			organizer.RemoveEmptyParents(dir, stopRoot)
		}
	}
	if cloudSource && drive != nil {
		drive.DeleteEmptyParents(ctx, sourceDir, stopRoot)
	} else if !cloudSource {
		organizer.RemoveEmptyParents(sourceDir, stopRoot)
	}
}

func cleanupStopRoot(dirs models.DirectoryConfig, sourceDir string, cloudSource bool) string {
	candidates := []string{
		dirs.IncomingPath,
		dirs.StagingPath,
		dirs.FailedPath,
		dirs.IncompleteCollectionsPath,
	}
	best := ""
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if cloudSource {
			root := clouddrive.NormalizePath(candidate)
			if cloudPathInside(root, sourceDir) && len(root) > len(best) {
				best = root
			}
			continue
		}
		root := filepath.Clean(candidate)
		if localPathInside(root, sourceDir) && len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		return best
	}
	if cloudSource {
		return cloudCleanupFallbackRoot(sourceDir)
	}
	return dirs.IncomingPath
}

func cloudCleanupFallbackRoot(sourceDir string) string {
	parts := strings.Split(strings.Trim(clouddrive.NormalizePath(sourceDir), "/"), "/")
	if len(parts) >= 2 {
		return "/" + path.Join(parts[0], parts[1])
	}
	if len(parts) == 1 && parts[0] != "" {
		return "/" + parts[0]
	}
	return "/"
}

func subtitleTarget(mediaTarget string, sidecar scanner.Sidecar, counts map[string]int) string {
	mediaExt := path.Ext(mediaTarget)
	if mediaExt == "" {
		mediaExt = filepath.Ext(mediaTarget)
	}
	base := strings.TrimSuffix(mediaTarget, mediaExt)
	ext := "." + strings.TrimPrefix(strings.ToLower(sidecar.Extension), ".")
	lang := subtitleLanguageSuffix(sidecar)
	key := lang + ext
	counts[key]++
	if counts[key] > 1 {
		return fmt.Sprintf("%s%s.%d%s", base, lang, counts[key], ext)
	}
	return base + lang + ext
}

func subtitleLanguageSuffix(sidecar scanner.Sidecar) string {
	if suffix := subtitleLanguageSuffixFromText(sidecar.Name); suffix != "" {
		return suffix
	}
	return subtitleLanguageSuffixFromText(sidecar.Path)
}

func subtitleLanguageSuffixFromText(text string) string {
	value := strings.ToLower(text)
	chs := hasSimplifiedSubtitleHint(value)
	cht := hasTraditionalSubtitleHint(value)
	switch {
	case chs && !cht:
		return ".chs"
	case cht && !chs:
		return ".cht"
	default:
		return ""
	}
}

func hasSimplifiedSubtitleHint(value string) bool {
	if chsSubtitleRE.MatchString(value) ||
		strings.Contains(value, "简体") ||
		strings.Contains(value, "簡體") ||
		strings.Contains(value, "简中") ||
		strings.Contains(value, "簡中") ||
		strings.Contains(value, "简日") ||
		strings.Contains(value, "日简") {
		return true
	}
	return hasSubtitleToken(value, chsSubtitleTokens)
}

func hasTraditionalSubtitleHint(value string) bool {
	if chtSubtitleRE.MatchString(value) ||
		strings.Contains(value, "繁体") ||
		strings.Contains(value, "繁體") ||
		strings.Contains(value, "繁中") ||
		strings.Contains(value, "繁日") ||
		strings.Contains(value, "日繁") {
		return true
	}
	return hasSubtitleToken(value, chtSubtitleTokens)
}

func hasSubtitleToken(value string, accepted map[string]struct{}) bool {
	for _, token := range subtitleTokens(value) {
		if _, ok := accepted[token]; ok {
			return true
		}
	}
	return false
}

func subtitleTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func (s *Service) migrateCompleteCloudCollectionLocked(ctx context.Context, dirs models.DirectoryConfig, result scraper.Result, values map[string]string) error {
	collectionTemplate, err := s.store.Template(ctx, models.TemplateCollectionMovie)
	if err != nil {
		return err
	}
	incompleteTemplate, err := s.store.Template(ctx, models.TemplateIncompleteCollection)
	if err != nil {
		return err
	}
	incompleteRenderTemplate := templateWithImplicitCategory(incompleteTemplate.Template, values)
	collectionRenderTemplate := templateWithImplicitCategory(collectionTemplate.Template, values)
	sourceRoot, err := naming.TemplateRoot(models.TemplateIncompleteCollection, incompleteRenderTemplate, values, dirs.IncompleteCollectionsPath)
	if err != nil {
		return err
	}
	targetRoot, err := naming.CollectionRoot(collectionRenderTemplate, values, dirs.StagingPath)
	if err != nil {
		return err
	}
	return s.moveCompleteCloudCollection(ctx, *result.Collection, sourceRoot, targetRoot)
}

func (s *Service) moveCompleteCloudCollection(ctx context.Context, collection models.CollectionMetadata, sourceRoot, targetRoot string) error {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return err
	}
	sourceURI := clouddrive.URI(sourceRoot)
	targetURI := clouddrive.URI(targetRoot)
	if err := clouddrive.New(settings).MigrateCollection(ctx, sourceRoot, targetRoot); err != nil {
		_ = s.store.AddHistory(ctx, uuid.NewString(), "", "", "", sourceURI, targetURI, "collection_complete_move", models.StatusFailed, models.ErrCollectionCompleteMoveFailed, err.Error())
		return err
	}
	batchIDs, err := s.store.UpdateCollectionPathPrefix(ctx, collection.TMDBID, sourceURI, targetURI)
	if err != nil {
		return err
	}
	for _, batchID := range batchIDs {
		_ = s.store.RecountBatch(ctx, batchID)
	}
	if len(batchIDs) > 0 {
		_ = s.store.AddCollectionCompletionHistory(ctx, uuid.NewString(), collection.TMDBID, collection.Name, sourceURI, targetURI, collection.MovieCount)
	}
	return nil
}

func repairCollectionSourceRoot(paths []string, incompleteRoot string, cloudSource bool) string {
	if cloudSource {
		return repairCloudCollectionSourceRoot(paths, incompleteRoot)
	}
	return repairLocalCollectionSourceRoot(paths, incompleteRoot)
}

func repairLocalCollectionSourceRoot(paths []string, incompleteRoot string) string {
	root := filepath.Clean(incompleteRoot)
	dirs := make([]string, 0, len(paths))
	for _, value := range paths {
		if strings.TrimSpace(value) == "" {
			continue
		}
		dirs = append(dirs, filepath.Dir(filepath.Clean(value)))
	}
	sourceRoot := commonLocalDir(dirs)
	if sourceRoot == "" {
		return ""
	}
	if len(dirs) == 1 {
		sourceRoot = filepath.Dir(sourceRoot)
	}
	if !localPathInside(root, sourceRoot) || filepath.Clean(sourceRoot) == root {
		return ""
	}
	return filepath.Clean(sourceRoot)
}

func repairCloudCollectionSourceRoot(paths []string, incompleteRoot string) string {
	root := clouddrive.NormalizePath(incompleteRoot)
	dirs := make([]string, 0, len(paths))
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if clouddrive.IsURI(value) {
			value = clouddrive.FromURI(value)
		}
		dirs = append(dirs, path.Dir(clouddrive.NormalizePath(value)))
	}
	sourceRoot := commonCloudDir(dirs)
	if sourceRoot == "" {
		return ""
	}
	if len(dirs) == 1 {
		sourceRoot = path.Dir(sourceRoot)
	}
	if !cloudPathInside(root, sourceRoot) || sourceRoot == root {
		return ""
	}
	return sourceRoot
}

func repairCollectionValues(collection models.CollectionMetadata, sourceRoot, incompleteRoot string, cloudSource bool) map[string]string {
	values := map[string]string{
		"collection_name": collection.Name,
		"collection_id":   strconv.Itoa(collection.TMDBID),
	}
	if category := repairCollectionCategory(sourceRoot, incompleteRoot, cloudSource); category != "" {
		values["category"] = category
	}
	return values
}

func repairCollectionCategory(sourceRoot, incompleteRoot string, cloudSource bool) string {
	if cloudSource {
		rel := strings.TrimPrefix(strings.TrimPrefix(sourceRoot, clouddrive.NormalizePath(incompleteRoot)), "/")
		parts := strings.Split(rel, "/")
		if len(parts) > 1 {
			return parts[0]
		}
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(incompleteRoot), filepath.Clean(sourceRoot))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}

func commonLocalDir(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	common := filepath.Clean(dirs[0])
	for _, dir := range dirs[1:] {
		dir = filepath.Clean(dir)
		for common != "" && common != "." && !localPathInside(common, dir) {
			next := filepath.Dir(common)
			if next == common {
				return ""
			}
			common = next
		}
	}
	return common
}

func commonCloudDir(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	common := clouddrive.NormalizePath(dirs[0])
	for _, dir := range dirs[1:] {
		dir = clouddrive.NormalizePath(dir)
		for common != "" && common != "/" && !cloudPathInside(common, dir) {
			next := path.Dir(common)
			if next == common {
				return ""
			}
			common = next
		}
	}
	return common
}

func localPathInside(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func cloudPathInside(root, target string) bool {
	root = strings.TrimRight(clouddrive.NormalizePath(root), "/")
	target = clouddrive.NormalizePath(target)
	return target == root || strings.HasPrefix(target, root+"/")
}

func cloudDriveDirs(settings models.CloudDriveSettings) models.DirectoryConfig {
	return models.DirectoryConfig{
		IncomingPath:              clouddrive.NormalizePath(settings.RootPath),
		StagingPath:               clouddrive.NormalizePath(settings.StagingPath),
		FailedPath:                clouddrive.NormalizePath(settings.FailedPath),
		IncompleteCollectionsPath: clouddrive.NormalizePath(settings.IncompleteCollectionsPath),
	}
}

func (s *Service) progress(ctx context.Context, batchID string) error {
	if s.redis == nil {
		return nil
	}
	batch, err := s.store.Batch(ctx, batchID)
	if err != nil {
		return err
	}
	return s.redis.HSet(ctx, "progress:batch:"+batchID, map[string]any{
		"total":                 batch.Total,
		"done":                  batch.Done,
		"failed":                batch.Failed,
		"incomplete_collection": batch.IncompleteCollection,
	}).Err()
}

func (s *Service) withQueue(ctx context.Context, queue, value string, fn func() error) error {
	if s.redis != nil {
		_ = s.redis.LPush(ctx, queue, value).Err()
		defer s.redis.LRem(context.Background(), queue, 1, value)
	}
	return fn()
}

func (s *Service) withLock(ctx context.Context, key string, ttl time.Duration, fn func() error) error {
	locked, unlock, err := s.acquireLock(ctx, key, ttl)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("%s 正在被其他任务占用", key)
	}
	defer unlock()
	return fn()
}

func (s *Service) acquireLock(ctx context.Context, key string, ttl time.Duration) (bool, func(), error) {
	if s.redis == nil {
		return true, func() {}, nil
	}
	locked, err := s.redis.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, func() {}, err
	}
	return locked, func() { _ = s.redis.Del(context.Background(), key).Err() }, nil
}

func namingValues(parsed parser.Result, technical models.MediaTechnicalInfo, result scraper.Result) map[string]string {
	values := map[string]string{
		"resolution":     technical.Resolution,
		"source":         parsed.Source,
		"video_codec":    technical.VideoCodec,
		"audio_codec":    technical.AudioCodec,
		"audio_channels": technical.AudioChannels,
		"hdr_format":     technical.HDRFormat,
		"extension":      parsed.Extension,
		"season":         strconv.Itoa(parsed.Season),
		"season_2":       parsed.Season2,
		"episode":        strconv.Itoa(parsed.Episode),
		"episode_2":      parsed.Episode2,
	}
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		values["title"] = result.Movie.Title
		values["year"] = strconv.Itoa(result.Movie.Year)
		if result.Collection != nil {
			values["collection_name"] = result.Collection.Name
			values["collection_id"] = strconv.Itoa(result.Collection.TMDBID)
		}
	case models.MediaTVEpisode:
		values["season"] = strconv.Itoa(result.TVEpisode.Season)
		values["season_2"] = fmt.Sprintf("%02d", result.TVEpisode.Season)
		values["episode"] = strconv.Itoa(result.TVEpisode.Episode)
		values["episode_2"] = fmt.Sprintf("%02d", result.TVEpisode.Episode)
		values["show_title"] = result.TVShow.Name
		values["show_year"] = strconv.Itoa(result.TVShow.Year)
		values["episode_title"] = result.TVEpisode.Title
	}
	return values
}

func classifierItem(result scraper.Result) classifier.Item {
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		return classifier.Item{
			GenreIDs:            splitCSV(result.Movie.GenreIDs),
			OriginalLanguage:    result.Movie.OriginalLanguage,
			ProductionCountries: splitCSV(result.Movie.ProductionCountries),
			Keywords:            splitCSV(result.Movie.Keywords),
		}
	case models.MediaTVEpisode:
		return classifier.Item{
			GenreIDs:         splitCSV(result.TVShow.GenreIDs),
			OriginalLanguage: result.TVShow.OriginalLanguage,
			OriginCountry:    splitCSV(result.TVShow.OriginCountry),
			Keywords:         splitCSV(result.TVShow.Keywords),
		}
	default:
		return classifier.Item{}
	}
}

func templateWithImplicitCategory(template string, values map[string]string) string {
	if strings.Contains(template, "{category}") || strings.TrimSpace(values["category"]) == "" {
		return template
	}
	template = strings.ReplaceAll(strings.TrimSpace(template), `\`, "/")
	parts := strings.Split(template, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return template
	}
	if isMediaTopDirectory(parts[0]) && len(parts) > 1 {
		return strings.Join(append([]string{parts[0], "{category}"}, parts[1:]...), "/")
	}
	return "{category}/" + template
}

func isMediaTopDirectory(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movies", "movie", "tv", "collections", "collection":
		return true
	default:
		return false
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func parseErr(err error) (string, string) {
	var target parser.ParseError
	if errors.As(err, &target) {
		return target.Code, target.Message
	}
	return models.ErrParseTitleEmpty, err.Error()
}

func scrapeErr(err error) (string, string) {
	var target scraper.Error
	if errors.As(err, &target) {
		return target.Code, target.Message
	}
	return models.ErrScrapeRequestFailed, err.Error()
}

func templateErrCode(err error) string {
	message := err.Error()
	if strings.Contains(message, "escape") {
		return models.ErrTemplatePathEscape
	}
	return models.ErrTemplateFieldInvalid
}

type codedWorkerError struct {
	code string
	err  error
}

func (e codedWorkerError) Error() string {
	if e.err == nil {
		return e.code
	}
	return e.err.Error()
}

func (e codedWorkerError) Unwrap() error {
	return e.err
}

func workerErrorCode(err error, fallback string) string {
	var coded codedWorkerError
	if errors.As(err, &coded) && coded.code != "" {
		return coded.code
	}
	return fallback
}
