package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"curio/internal/clouddrive"
	"curio/internal/config"
	"curio/internal/mediainfo"
	"curio/internal/repository"
	"curio/internal/scanner"

	"github.com/jackc/pgx/v5/pgxpool"
)

type result struct {
	file   scanner.File
	info   mediainfo.Info
	timing probeTiming
	err    error
}

type probeTiming struct {
	Prefetch time.Duration
	Download time.Duration
	Close    time.Duration
	Probe    mediainfo.ProbeStats
	Mode     string
}

func main() {
	var root string
	var concurrency int
	var limit int
	var sample int
	var seed int64
	var verbose bool
	var modeValue string
	var prefetch bool
	flag.StringVar(&root, "path", "", "CloudDrive2 path to probe")
	flag.IntVar(&concurrency, "concurrency", 2, "max probe concurrency, capped at 2")
	flag.IntVar(&limit, "limit", 0, "optional max files")
	flag.IntVar(&sample, "sample", 0, "random sample size")
	flag.Int64Var(&seed, "seed", time.Now().UnixNano(), "random sample seed")
	flag.BoolVar(&verbose, "v", false, "print every file")
	flag.StringVar(&modeValue, "probe-mode", string(clouddrive.DownloadModeAuto), "download mode: auto, direct, proxy")
	flag.BoolVar(&prefetch, "prefetch", false, "prefetch the first 16MB for proxy ISO probing")
	flag.Parse()
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 2 {
		concurrency = 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Load()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		exit(err)
	}
	defer db.Close()
	store := repository.New(db)
	settings, err := store.CloudDriveSettings(ctx)
	if err != nil {
		exit(err)
	}
	if strings.TrimSpace(root) == "" {
		root = settings.RootPath
	}
	drive, err := clouddrive.New(settings).Open(ctx)
	if err != nil {
		exit(err)
	}
	defer drive.Close()

	mode := clouddrive.NormalizeDownloadMode(modeValue)
	start := time.Now()
	scanStart := time.Now()
	files, err := drive.Scan(ctx, root)
	if err != nil {
		exit(err)
	}
	scanElapsed := time.Since(scanStart)
	totalFiles := len(files)
	if sample > 0 && sample < len(files) {
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
		files = files[:sample]
	}
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	fmt.Printf("probe path=%s total=%d files=%d concurrency=%d seed=%d mode=%s prefetch=%v scan=%s\n", root, totalFiles, len(files), concurrency, seed, mode, prefetch, round(scanElapsed))

	jobs := make(chan scanner.File)
	results := make(chan result)
	var done atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				results <- probe(ctx, drive, file, mode, prefetch)
				count := done.Add(1)
				if count%25 == 0 {
					fmt.Printf("progress %d/%d elapsed=%s\n", count, len(files), time.Since(start).Round(time.Second))
				}
			}
		}()
	}
	go func() {
		for _, file := range files {
			jobs <- file
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var failed, missing int
	var totalTiming probeTiming
	for item := range results {
		totalTiming.Prefetch += item.timing.Prefetch
		totalTiming.Download += item.timing.Download
		totalTiming.Close += item.timing.Close
		totalTiming.Probe.Total += item.timing.Probe.Total
		totalTiming.Probe.ISOParse += item.timing.Probe.ISOParse
		totalTiming.Probe.ISOSample += item.timing.Probe.ISOSample
		totalTiming.Probe.FFProbe += item.timing.Probe.FFProbe
		totalTiming.Probe.RangeRead += item.timing.Probe.RangeRead
		totalTiming.Probe.RangeRequests += item.timing.Probe.RangeRequests
		totalTiming.Probe.RangeBytes += item.timing.Probe.RangeBytes
		totalTiming.Probe.ISOAttempts += item.timing.Probe.ISOAttempts
		totalTiming.Probe.ISOSampleBytes += item.timing.Probe.ISOSampleBytes
		if item.err != nil {
			failed++
			fmt.Printf("FAIL %s | %s\n", item.err, item.file.Path)
			continue
		}
		if incomplete(item.info) {
			missing++
			fmt.Printf("MISS %+v | %s\n", item.info, item.file.Path)
			continue
		}
		if verbose {
			fmt.Printf("OK %+v | mode=%s total=%s prefetch=%s url=%s iso_parse=%s sample=%s ffprobe=%s range=%d/%s/%s attempts=%d | %s\n",
				item.info,
				item.timing.Mode,
				round(item.timing.Probe.Total),
				round(item.timing.Prefetch),
				round(item.timing.Download),
				round(item.timing.Probe.ISOParse),
				round(item.timing.Probe.ISOSample),
				round(item.timing.Probe.FFProbe),
				item.timing.Probe.RangeRequests,
				bytesText(item.timing.Probe.RangeBytes),
				round(item.timing.Probe.RangeRead),
				item.timing.Probe.ISOAttempts,
				item.file.Path)
		}
	}
	fmt.Printf("summary files=%d ok=%d missing=%d failed=%d elapsed=%s\n", len(files), len(files)-missing-failed, missing, failed, time.Since(start).Round(time.Second))
	fmt.Printf("timing scan=%s prefetch=%s url=%s probe=%s iso_parse=%s sample=%s ffprobe=%s range=%d/%s/%s attempts=%d sample_bytes=%s\n",
		round(scanElapsed),
		round(totalTiming.Prefetch),
		round(totalTiming.Download),
		round(totalTiming.Probe.Total),
		round(totalTiming.Probe.ISOParse),
		round(totalTiming.Probe.ISOSample),
		round(totalTiming.Probe.FFProbe),
		totalTiming.Probe.RangeRequests,
		bytesText(totalTiming.Probe.RangeBytes),
		round(totalTiming.Probe.RangeRead),
		totalTiming.Probe.ISOAttempts,
		bytesText(totalTiming.Probe.ISOSampleBytes))
	if failed > 0 || missing > 0 {
		os.Exit(1)
	}
}

func probe(ctx context.Context, drive *clouddrive.DriveSession, file scanner.File, mode clouddrive.DownloadMode, prefetch bool) result {
	var timing probeTiming
	if strings.EqualFold(file.Extension, "iso") && mode == clouddrive.DownloadModeProxy && prefetch {
		start := time.Now()
		_ = drive.Prefetch(ctx, file.Path, []clouddrive.ByteRange{{Start: 0, Length: 16 * 1024 * 1024}})
		timing.Prefetch = time.Since(start)
	}
	start := time.Now()
	download, err := drive.DownloadURLWithMode(ctx, file.Path, mode)
	timing.Download = time.Since(start)
	if err != nil {
		return result{file: file, timing: timing, err: err}
	}
	timing.Mode = download.Mode
	info, stats, err := mediainfo.ProbeWithStats(ctx, mediainfo.Source{
		URL:       mediainfo.CleanURL(download.URL),
		Headers:   download.Headers,
		UserAgent: download.UserAgent,
		Extension: file.Extension,
		Size:      file.Size,
	})
	timing.Probe = stats
	if download.Mode == string(clouddrive.DownloadModeProxy) {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeStart := time.Now()
		_ = drive.CloseFileReader(closeCtx, file.Path)
		timing.Close = time.Since(closeStart)
		cancel()
	}
	return result{file: file, info: info, timing: timing, err: err}
}

func incomplete(info mediainfo.Info) bool {
	return info.Resolution == mediainfo.Unknown ||
		info.VideoCodec == mediainfo.Unknown ||
		info.AudioCodec == mediainfo.Unknown ||
		info.AudioChannels == mediainfo.Unknown ||
		info.HDRFormat == mediainfo.Unknown
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func round(value time.Duration) time.Duration {
	if value > time.Second {
		return value.Round(time.Millisecond)
	}
	return value.Round(time.Microsecond)
}

func bytesText(value int64) string {
	const mb = 1024 * 1024
	if value >= mb {
		return fmt.Sprintf("%.1fMB", float64(value)/mb)
	}
	return fmt.Sprintf("%dB", value)
}
