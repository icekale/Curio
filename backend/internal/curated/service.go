package curated

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"curio/internal/models"
	"curio/internal/repository"
	"curio/internal/scraper"

	"golang.org/x/net/html"
)

const (
	doubanTop250URL       = "https://movie.douban.com/top250"
	doubanTop250RexxarURL = "https://m.douban.com/rexxar/api/v2/subject_collection/movie_top250/items"
	doubanHTTPTimeout     = 30 * time.Second
	doubanRefreshPeriod   = 24 * time.Hour
)

var (
	doubanSubjectRE = regexp.MustCompile(`/subject/(\d+)/?`)
	yearRE          = regexp.MustCompile(`\b(18|19|20)\d{2}\b`)
)

type Service struct {
	store   *repository.Store
	scrape  *scraper.Client
	refresh sync.Mutex
}

type doubanMovie struct {
	Rank          int
	DoubanID      string
	Title         string
	OriginalTitle string
	Year          int
	Rating        string
	SourceURL     string
	PosterPath    string
	SearchTitles  []string
}

type doubanTop250RexxarResponse struct {
	Start                  int                 `json:"start"`
	Count                  int                 `json:"count"`
	Total                  int                 `json:"total"`
	SubjectCollectionItems []doubanRexxarMovie `json:"subject_collection_items"`
}

type doubanRexxarMovie struct {
	ID           string `json:"id"`
	Rank         int    `json:"rank"`
	RankValue    int    `json:"rank_value"`
	Title        string `json:"title"`
	CardSubtitle string `json:"card_subtitle"`
	CoverURL     string `json:"cover_url"`
	URL          string `json:"url"`
	Rating       struct {
		Value float64 `json:"value"`
	} `json:"rating"`
	Pic struct {
		Large  string `json:"large"`
		Normal string `json:"normal"`
	} `json:"pic"`
}

func New(store *repository.Store, scraperClient *scraper.Client) *Service {
	return &Service{store: store, scrape: scraperClient}
}

func (s *Service) StartScheduler(ctx context.Context) {
	go s.scheduler(ctx)
}

func (s *Service) scheduler(ctx context.Context) {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refreshTop250IfDue(ctx)
			timer.Reset(time.Hour)
		}
	}
}

func (s *Service) refreshTop250IfDue(ctx context.Context) {
	collection, err := s.store.CuratedCollection(ctx, models.CuratedDoubanTop250ID)
	if err != nil {
		log.Printf("curio curated douban top250 due check failed: %v", err)
		return
	}
	if collection.LastRefreshedAt != nil && time.Since(*collection.LastRefreshedAt) < doubanRefreshPeriod && collection.MovieCount > 0 {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	if _, err := s.RefreshTop250(runCtx); err != nil {
		log.Printf("curio curated douban top250 refresh failed: %v", err)
	}
}

func (s *Service) RefreshTop250(ctx context.Context) (models.CollectionMetadata, error) {
	s.refresh.Lock()
	defer s.refresh.Unlock()

	client := doubanHTTPClient()
	started := time.Now()
	items, err := fetchTop250(ctx, client)
	if err != nil {
		_ = s.store.MarkCuratedCollectionRefreshError(ctx, models.CuratedDoubanTop250ID, err.Error())
		return models.CollectionMetadata{}, err
	}
	existing, err := s.store.CuratedCollectionMovieMap(ctx, models.CuratedDoubanTop250ID)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	movies := make([]models.CollectionMovieMetadata, 0, len(items))
	for _, item := range items {
		select {
		case <-ctx.Done():
			return models.CollectionMetadata{}, ctx.Err()
		default:
		}
		next := item.toCollectionMovie()
		if previous, ok := existing[item.DoubanID]; ok && previous.MovieTMDBID > 0 {
			copyResolvedFields(&next, previous)
			movies = append(movies, next)
			continue
		} else if ok && previous.IMDBID != "" {
			next.IMDBID = previous.IMDBID
		}
		if s.scrape != nil {
			movie, ok, resolveErr := s.scrape.ResolveMovie(ctx, next.IMDBID, item.SearchTitles, item.Year)
			if resolveErr != nil {
				next.ErrorMessage = resolveErr.Error()
			} else if ok {
				applyMovieMetadata(&next, movie)
				_ = s.store.UpsertMovie(ctx, movie)
			}
			sleepContext(ctx, 80*time.Millisecond)
		}
		if next.MovieTMDBID > 0 {
			next.MatchStatus = "matched"
			next.ErrorMessage = ""
		} else {
			next.MatchStatus = "unresolved"
			if next.ErrorMessage == "" {
				next.ErrorMessage = "未匹配到 TMDB 电影"
			}
		}
		movies = append(movies, next)
	}
	if err := s.store.ReplaceCuratedCollectionMovies(ctx, models.CuratedDoubanTop250ID, movies); err != nil {
		_ = s.store.MarkCuratedCollectionRefreshError(ctx, models.CuratedDoubanTop250ID, err.Error())
		return models.CollectionMetadata{}, err
	}
	collection, err := s.store.CuratedCollection(ctx, models.CuratedDoubanTop250ID)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	log.Printf("curio curated douban top250 refreshed items=%d local=%d unresolved=%d elapsed_ms=%d",
		collection.MovieCount, collection.LocalCount, collection.UnresolvedCount, time.Since(started).Milliseconds())
	return collection, nil
}

func (m doubanMovie) toCollectionMovie() models.CollectionMovieMetadata {
	return models.CollectionMovieMetadata{
		ListID:        models.CuratedDoubanTop250ID,
		SortOrder:     m.Rank,
		DoubanID:      m.DoubanID,
		Title:         m.Title,
		OriginalTitle: m.OriginalTitle,
		Year:          m.Year,
		Rating:        m.Rating,
		SourceURL:     m.SourceURL,
		PosterPath:    m.PosterPath,
		Released:      true,
		MatchStatus:   "pending",
	}
}

func copyResolvedFields(target *models.CollectionMovieMetadata, previous models.CollectionMovieMetadata) {
	target.IMDBID = firstNonEmpty(target.IMDBID, previous.IMDBID)
	target.MovieTMDBID = previous.MovieTMDBID
	target.OriginalTitle = firstNonEmpty(target.OriginalTitle, previous.OriginalTitle)
	target.ReleaseDate = previous.ReleaseDate
	target.PosterPath = previous.PosterPath
	target.BackdropPath = previous.BackdropPath
	target.MatchStatus = "matched"
	target.Resolved = true
	target.ErrorMessage = ""
}

func applyMovieMetadata(target *models.CollectionMovieMetadata, movie models.MovieMetadata) {
	target.MovieTMDBID = movie.TMDBID
	target.IMDBID = firstNonEmpty(target.IMDBID, movie.IMDBID)
	target.OriginalTitle = firstNonEmpty(target.OriginalTitle, movie.Original)
	target.ReleaseDate = movie.ReleaseDate
	target.PosterPath = movie.PosterPath
	target.BackdropPath = movie.BackdropPath
	target.Resolved = true
}

func fetchTop250(ctx context.Context, client *http.Client) ([]doubanMovie, error) {
	items, err := fetchTop250Rexxar(ctx, client)
	if err == nil {
		return items, nil
	}
	htmlItems, htmlErr := fetchTop250HTML(ctx, client)
	if htmlErr == nil {
		return htmlItems, nil
	}
	return nil, fmt.Errorf("豆瓣 Top250 JSON 源失败: %v; HTML 源失败: %v", err, htmlErr)
}

func fetchTop250Rexxar(ctx context.Context, client *http.Client) ([]doubanMovie, error) {
	items := make([]doubanMovie, 0, 250)
	seen := map[string]struct{}{}
	for start := 0; start < 250; start += 50 {
		rawURL := fmt.Sprintf("%s?start=%d&count=50&items_only=1&for_mobile=1", doubanTop250RexxarURL, start)
		body, err := fetchRexxarURL(ctx, client, rawURL)
		if err != nil {
			return nil, err
		}
		var response doubanTop250RexxarResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, err
		}
		for _, entry := range response.SubjectCollectionItems {
			item := entry.toMovie()
			if item.DoubanID == "" {
				continue
			}
			if _, ok := seen[item.DoubanID]; ok {
				continue
			}
			seen[item.DoubanID] = struct{}{}
			if item.Rank <= 0 {
				item.Rank = len(items) + 1
			}
			items = append(items, item)
		}
		if len(response.SubjectCollectionItems) == 0 {
			break
		}
		sleepContext(ctx, 120*time.Millisecond)
	}
	if len(items) < 250 {
		return nil, fmt.Errorf("豆瓣 Top250 JSON 源返回数量不足: %d/250", len(items))
	}
	return items[:250], nil
}

func (m doubanRexxarMovie) toMovie() doubanMovie {
	rank := m.Rank
	if rank <= 0 {
		rank = m.RankValue
	}
	sourceURL := strings.TrimSpace(m.URL)
	if sourceURL == "" && strings.TrimSpace(m.ID) != "" {
		sourceURL = "https://movie.douban.com/subject/" + strings.TrimSpace(m.ID) + "/"
	}
	rating := ""
	if m.Rating.Value > 0 {
		rating = strconv.FormatFloat(m.Rating.Value, 'f', 1, 64)
	}
	poster := firstNonEmpty(m.CoverURL, m.Pic.Large, m.Pic.Normal)
	title := cleanTitle(m.Title)
	searchTitles := uniqueNonEmpty([]string{title})
	return doubanMovie{
		Rank:         rank,
		DoubanID:     strings.TrimSpace(m.ID),
		Title:        title,
		Year:         firstYear(m.CardSubtitle),
		Rating:       rating,
		SourceURL:    sourceURL,
		SearchTitles: searchTitles,
		PosterPath:   poster,
	}
}

func fetchTop250HTML(ctx context.Context, client *http.Client) ([]doubanMovie, error) {
	items := make([]doubanMovie, 0, 250)
	seen := map[string]struct{}{}
	for start := 0; start < 250; start += 25 {
		pageURL := fmt.Sprintf("%s?start=%d&filter=", doubanTop250URL, start)
		body, err := fetchURL(ctx, client, pageURL)
		if err != nil {
			return nil, err
		}
		pageItems, err := parseTop250Page(body)
		if err != nil {
			return nil, err
		}
		for _, item := range pageItems {
			if item.DoubanID == "" {
				continue
			}
			if _, ok := seen[item.DoubanID]; ok {
				continue
			}
			seen[item.DoubanID] = struct{}{}
			if item.Rank <= 0 {
				item.Rank = len(items) + 1
			}
			items = append(items, item)
		}
		sleepContext(ctx, 200*time.Millisecond)
	}
	if len(items) < 250 {
		return nil, fmt.Errorf("豆瓣 Top250 HTML 源返回数量不足: %d/250", len(items))
	}
	return items[:250], nil
}

func fetchURL(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", doubanTop250URL)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("豆瓣请求失败: %s %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func fetchRexxarURL(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", "https://m.douban.com/subject_collection/movie_top250/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("豆瓣 JSON 请求失败: %s %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func parseTop250Page(body []byte) ([]doubanMovie, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodes := findNodes(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "item")
	})
	items := make([]doubanMovie, 0, len(nodes))
	for _, node := range nodes {
		item := parseTop250Item(node)
		if item.DoubanID != "" && item.Title != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, errors.New("豆瓣 Top250 页面没有解析到影片")
	}
	return items, nil
}

func parseTop250Item(node *html.Node) doubanMovie {
	var item doubanMovie
	if rankNode := firstNode(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "em"
	}); rankNode != nil {
		item.Rank, _ = strconv.Atoi(strings.TrimSpace(nodeText(rankNode)))
	}
	link := firstNode(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attr(n, "href"), "/subject/")
	})
	if link != nil {
		item.SourceURL = attr(link, "href")
		item.DoubanID = doubanIDFromURL(item.SourceURL)
	}
	titleNodes := findNodes(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "title")
	})
	titles := make([]string, 0, len(titleNodes)+4)
	for _, titleNode := range titleNodes {
		if title := cleanTitle(nodeText(titleNode)); title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) > 0 {
		item.Title = titles[0]
	}
	if len(titles) > 1 {
		item.OriginalTitle = titles[1]
	}
	if otherNode := firstNode(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "other")
	}); otherNode != nil {
		titles = append(titles, splitOtherTitles(nodeText(otherNode))...)
	}
	if ratingNode := firstNode(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "rating_num")
	}); ratingNode != nil {
		item.Rating = strings.TrimSpace(nodeText(ratingNode))
	}
	if bdNode := firstNode(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bd")
	}); bdNode != nil {
		item.Year = firstYear(nodeText(bdNode))
	}
	if item.Year == 0 {
		item.Year = firstYear(nodeText(node))
	}
	item.SearchTitles = uniqueNonEmpty(titles)
	return item
}

func findNodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			out = append(out, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func firstNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := firstNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func attr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			builder.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return normalizeSpace(builder.String())
}

func doubanIDFromURL(value string) string {
	match := doubanSubjectRE.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func cleanTitle(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "/ ")
	return normalizeSpace(value)
}

func splitOtherTitles(value string) []string {
	parts := strings.Split(value, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = cleanTitle(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstYear(value string) int {
	match := yearRE.FindString(value)
	if match == "" {
		return 0
	}
	year, _ := strconv.Atoi(match)
	return year
}

func uniqueNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = cleanTitle(value)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeSpace(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\u00a0", " ")), " ")
}

func sleepContext(ctx context.Context, duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func doubanHTTPClient() *http.Client {
	// Douban is sensitive to shared proxy exits, so these requests deliberately
	// ignore Curio's network_proxy and HTTP(S)_PROXY environment variables.
	transport := &http.Transport{Proxy: nil}
	return &http.Client{Timeout: doubanHTTPTimeout, Transport: transport}
}
