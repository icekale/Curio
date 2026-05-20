package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"curio/internal/config"
	"curio/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db *pgxpool.Pool
}

type CollectionRepairCandidate struct {
	Collection models.CollectionMetadata
	FilePaths  []string
}

func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS system_directories (
			id INT PRIMARY KEY,
			incoming_path TEXT NOT NULL,
			staging_path TEXT NOT NULL,
			failed_path TEXT NOT NULL,
			incomplete_collections_path TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS system_settings (
			id INT PRIMARY KEY,
			tmdb_api_key TEXT NOT NULL DEFAULT '',
			network_proxy TEXT NOT NULL DEFAULT '',
			classification_yaml TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS classification_yaml TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_filename_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_filename_force BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_base_url TEXT NOT NULL DEFAULT 'https://api.openai.com/v1'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_api_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_model TEXT NOT NULL DEFAULT 'gpt-5.5'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS ai_filename_prompt TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_address TEXT NOT NULL DEFAULT 'http://localhost:19798'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_password TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_root_path TEXT NOT NULL DEFAULT '/'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_staging_path TEXT NOT NULL DEFAULT '/Curio/staging'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_failed_path TEXT NOT NULL DEFAULT '/Curio/failed'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_incomplete_path TEXT NOT NULL DEFAULT '/Curio/incomplete_collections'`,
		`CREATE TABLE IF NOT EXISTS p115_settings (
			id INT PRIMARY KEY,
			enabled BOOLEAN NOT NULL DEFAULT true,
			auth_mode TEXT NOT NULL DEFAULT 'cookies',
			app_id TEXT NOT NULL DEFAULT '',
			app_secret TEXT NOT NULL DEFAULT '',
			access_token TEXT NOT NULL DEFAULT '',
			refresh_token TEXT NOT NULL DEFAULT '',
			cookies TEXT NOT NULL DEFAULT '',
			cookie_login_app TEXT NOT NULL DEFAULT 'wechatmini',
			strm_output_path TEXT NOT NULL DEFAULT '/data/Curio/strm',
			public_base_url TEXT NOT NULL DEFAULT '',
			direct_url_ttl_seconds INT NOT NULL DEFAULT 3000,
			user_agent_mode TEXT NOT NULL DEFAULT 'inherit',
			fixed_user_agent TEXT NOT NULL DEFAULT '',
			library_cid TEXT NOT NULL DEFAULT '',
			delete_missing_strm BOOLEAN NOT NULL DEFAULT true,
			stale_before_delete BOOLEAN NOT NULL DEFAULT false,
			keep_deleted_days INT NOT NULL DEFAULT 7,
			refresh_emby_after_sync BOOLEAN NOT NULL DEFAULT false,
			sync_cron_enabled BOOLEAN NOT NULL DEFAULT false,
			sync_interval_minutes INT NOT NULL DEFAULT 60,
			emby_upstream_url TEXT NOT NULL DEFAULT '',
			emby_public_url TEXT NOT NULL DEFAULT '',
			emby_proxy_port INT NOT NULL DEFAULT 8097,
			emby_proxy_base_path TEXT NOT NULL DEFAULT '/emby',
			emby_api_key TEXT NOT NULL DEFAULT '',
			open_token_refreshed_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS cookie_login_app TEXT NOT NULL DEFAULT 'wechatmini'`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS emby_proxy_port INT NOT NULL DEFAULT 8097`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS sync_cron_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS sync_interval_minutes INT NOT NULL DEFAULT 60`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS library_cid TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS open_token_refreshed_at TIMESTAMPTZ`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name='p115_settings' AND column_name='libraries_yaml'
			) THEN
				UPDATE p115_settings
				SET library_cid = btrim(libraries_yaml)
				WHERE library_cid = ''
					AND btrim(libraries_yaml) <> ''
					AND btrim(libraries_yaml) !~ '[[:space:]:]';
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS strm_links (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			library_cid TEXT NOT NULL,
			library_name TEXT NOT NULL DEFAULT '',
			library_type TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL,
			remote_path TEXT NOT NULL DEFAULT '',
			remote_file_id TEXT NOT NULL DEFAULT '',
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			strm_path TEXT NOT NULL,
			play_path TEXT NOT NULL DEFAULT '',
			source_tree_hash TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			resolve_status TEXT NOT NULL DEFAULT 'pending',
			status TEXT NOT NULL DEFAULT 'generated',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			media_streams_json TEXT NOT NULL DEFAULT '',
			media_duration_ticks BIGINT NOT NULL DEFAULT 0,
			media_probed_at TIMESTAMPTZ,
			media_probe_error TEXT NOT NULL DEFAULT '',
			generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			resolved_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, library_cid, relative_path)
		)`,
		`ALTER TABLE strm_links ADD COLUMN IF NOT EXISTS media_streams_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE strm_links ADD COLUMN IF NOT EXISTS media_duration_ticks BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE strm_links ADD COLUMN IF NOT EXISTS media_probed_at TIMESTAMPTZ`,
		`ALTER TABLE strm_links ADD COLUMN IF NOT EXISTS media_probe_error TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_strm_links_path ON strm_links(strm_path)`,
		`CREATE INDEX IF NOT EXISTS idx_strm_links_library ON strm_links(provider, library_cid, status)`,
		`CREATE TABLE IF NOT EXISTS p115_tree_snapshots (
			library_cid TEXT NOT NULL,
			content_key TEXT NOT NULL,
			tree_version TEXT NOT NULL,
			relative_path TEXT NOT NULL,
			name TEXT NOT NULL,
			remote_file_id TEXT NOT NULL DEFAULT '',
			parent_file_id TEXT NOT NULL DEFAULT '',
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			extension TEXT NOT NULL DEFAULT '',
			depth INT NOT NULL DEFAULT 0,
			is_directory BOOLEAN NOT NULL DEFAULT false,
			is_media BOOLEAN NOT NULL DEFAULT false,
			source_tree_hash TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(library_cid, content_key)
		)`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS remote_file_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS parent_file_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS pickcode TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS sha1 TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS size BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS is_directory BOOLEAN NOT NULL DEFAULT false`,
		`CREATE INDEX IF NOT EXISTS idx_p115_tree_snapshots_version ON p115_tree_snapshots(library_cid, tree_version)`,
		`CREATE TABLE IF NOT EXISTS p115_nodes (
			library_cid TEXT NOT NULL,
			remote_file_id TEXT NOT NULL,
			parent_file_id TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			is_directory BOOLEAN NOT NULL DEFAULT false,
			is_media BOOLEAN NOT NULL DEFAULT false,
			is_alive BOOLEAN NOT NULL DEFAULT true,
			source_tree_hash TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(library_cid, remote_file_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_alive ON p115_nodes(library_cid, is_alive)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_parent ON p115_nodes(library_cid, parent_file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_path ON p115_nodes(library_cid, relative_path)`,
		`CREATE TABLE IF NOT EXISTS p115_event_cursors (
			library_cid TEXT PRIMARY KEY,
			last_event_id BIGINT NOT NULL DEFAULT 0,
			last_event_time BIGINT NOT NULL DEFAULT 0,
			last_sync_status TEXT NOT NULL DEFAULT '',
			last_sync_error TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS p115_sync_runs (
			id TEXT PRIMARY KEY,
			trigger TEXT NOT NULL DEFAULT 'manual_sync',
			status TEXT NOT NULL DEFAULT 'running',
			mode TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			exported INT NOT NULL DEFAULT 0,
			generated INT NOT NULL DEFAULT 0,
			restored INT NOT NULL DEFAULT 0,
			updated_count INT NOT NULL DEFAULT 0,
			deleted_count INT NOT NULL DEFAULT 0,
			skipped INT NOT NULL DEFAULT 0,
			failed INT NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			event_summary TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ended_at TIMESTAMPTZ,
			duration_ms BIGINT NOT NULL DEFAULT 0
		)`,
		`ALTER TABLE p115_sync_runs ADD COLUMN IF NOT EXISTS event_summary TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_p115_sync_runs_started ON p115_sync_runs(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_sync_runs_trigger ON p115_sync_runs(trigger, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS emby_strm_items (
			id TEXT PRIMARY KEY,
			emby_server_id TEXT NOT NULL DEFAULT 'default',
			emby_item_id TEXT NOT NULL,
			strm_link_id TEXT NOT NULL REFERENCES strm_links(id) ON DELETE CASCADE,
			strm_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(emby_server_id, emby_item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS emby_playback_progress (
			id TEXT PRIMARY KEY,
			emby_server_id TEXT NOT NULL DEFAULT 'default',
			user_id TEXT NOT NULL,
			emby_item_id TEXT NOT NULL,
			strm_link_id TEXT NOT NULL DEFAULT '',
			position_ticks BIGINT NOT NULL DEFAULT 0,
			duration_ticks BIGINT NOT NULL DEFAULT 0,
			played BOOLEAN NOT NULL DEFAULT false,
			client TEXT NOT NULL DEFAULT '',
			device TEXT NOT NULL DEFAULT '',
			play_session_id TEXT NOT NULL DEFAULT '',
			last_event TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			cleared_at TIMESTAMPTZ,
			UNIQUE(emby_server_id, user_id, emby_item_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_emby_playback_progress_recent
			ON emby_playback_progress(emby_server_id, user_id, updated_at DESC)
			WHERE cleared_at IS NULL AND played=false`,
		`CREATE TABLE IF NOT EXISTS naming_templates (
			template_type TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			template TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`UPDATE naming_templates SET template='movies/{category}/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='movie' AND template='movies/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='tv/{category}/{show_title} ({show_year})/Season {season_2}/{show_title} - S{season_2}E{episode_2} - {episode_title} - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='tv_episode' AND template='tv/{show_title} ({show_year})/Season {season_2}/{show_title} - S{season_2}E{episode_2} - {episode_title} - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='collections/{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='collection_movie' AND template='collections/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='incomplete_collection_movie' AND template='{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'movies/%' THEN 'movies/{category}/' || substring(template from 8)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='movie' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'tv/%' THEN 'tv/{category}/' || substring(template from 4)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='tv_episode' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'collections/%' THEN 'collections/{category}/' || substring(template from 13)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='collection_movie' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template='{category}/' || template, updated_at=now()
			WHERE template_type='incomplete_collection_movie' AND template NOT LIKE '%{category}%'`,
		`CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT 'local',
			status TEXT NOT NULL,
			total INT NOT NULL DEFAULT 0,
			done INT NOT NULL DEFAULT 0,
			failed INT NOT NULL DEFAULT 0,
			incomplete_collection INT NOT NULL DEFAULT 0,
			started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ended_at TIMESTAMPTZ
		)`,
		`ALTER TABLE batches ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'local'`,
		`CREATE INDEX IF NOT EXISTS idx_batches_active ON batches(status, ended_at) WHERE ended_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_batches_started ON batches(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS media_files (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
			original_path TEXT NOT NULL,
			current_path TEXT NOT NULL,
			final_path TEXT NOT NULL DEFAULT '',
			original_file_name TEXT NOT NULL,
			final_file_name TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL,
			file_size BIGINT NOT NULL,
			file_hash TEXT NOT NULL DEFAULT '',
			hash_type TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL DEFAULT '',
			process_status TEXT NOT NULL,
			match_status TEXT NOT NULL DEFAULT '',
			parse_title TEXT NOT NULL DEFAULT '',
			parse_year INT,
			season INT,
			episode INT,
			resolution TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			video_codec TEXT NOT NULL DEFAULT '',
			audio_codec TEXT NOT NULL DEFAULT '',
			audio_channels TEXT NOT NULL DEFAULT '',
			hdr_format TEXT NOT NULL DEFAULT '',
			planned_target TEXT NOT NULL DEFAULT '',
			move_attempts INT NOT NULL DEFAULT 0,
			last_verified_path TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS planned_target TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS move_attempts INT NOT NULL DEFAULT 0`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS last_verified_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS audio_codec TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS audio_channels TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS hdr_format TEXT NOT NULL DEFAULT ''`,
		`UPDATE media_files SET video_codec='AVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H264', 'X264', 'AVC')`,
		`UPDATE media_files SET video_codec='HEVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H265', 'X265', 'HEVC')`,
		`UPDATE media_files SET video_codec='VVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H266', 'X266', 'VVC')`,
		`UPDATE media_files SET video_codec='MPEG-2' WHERE upper(replace(video_codec, '-', ''))='MPEG2'`,
		`UPDATE media_files SET video_codec='MPEG-4' WHERE upper(replace(video_codec, '-', ''))='MPEG4'`,
		`UPDATE media_files SET video_codec='VC-1' WHERE upper(replace(video_codec, '-', ''))='VC1'`,
		`DELETE FROM media_files WHERE process_status='failed' AND error_code='UNSUPPORTED_EXTENSION' AND media_type=''`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_batch ON media_files(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_status ON media_files(process_status)`,
		`CREATE TABLE IF NOT EXISTS ai_filename_logs (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL DEFAULT '',
			file_id TEXT NOT NULL DEFAULT '',
			file_path TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			proxy_url TEXT NOT NULL DEFAULT '',
			response_format TEXT NOT NULL DEFAULT '',
			request_json TEXT NOT NULL DEFAULT '',
			response_json TEXT NOT NULL DEFAULT '',
			parsed_json TEXT NOT NULL DEFAULT '',
			http_status INT NOT NULL DEFAULT 0,
			duration_ms BIGINT NOT NULL DEFAULT 0,
			attempt INT NOT NULL DEFAULT 0,
			media_type TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			season INT NOT NULL DEFAULT 0,
			episode INT NOT NULL DEFAULT 0,
			confidence NUMERIC NOT NULL DEFAULT 0,
			needs_review BOOLEAN NOT NULL DEFAULT false,
			reason TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_filename_logs_created ON ai_filename_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_filename_logs_batch ON ai_filename_logs(batch_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_filename_logs_file ON ai_filename_logs(file_id)`,
		`CREATE TABLE IF NOT EXISTS movies (
			tmdb_id INT PRIMARY KEY,
			imdb_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL,
			original_title TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			release_date TEXT NOT NULL DEFAULT '',
			overview TEXT NOT NULL DEFAULT '',
			runtime INT NOT NULL DEFAULT 0,
			genres TEXT NOT NULL DEFAULT '',
			genre_ids TEXT NOT NULL DEFAULT '',
			original_language TEXT NOT NULL DEFAULT '',
			production_countries TEXT NOT NULL DEFAULT '',
			keywords TEXT NOT NULL DEFAULT '',
			rating TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			collection_id INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS genre_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS original_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS production_countries TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS keywords TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS tv_shows (
			tmdb_id INT PRIMARY KEY,
			tvdb_id INT NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			original_name TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			first_air_date TEXT NOT NULL DEFAULT '',
			overview TEXT NOT NULL DEFAULT '',
			season_count INT NOT NULL DEFAULT 0,
			episode_count INT NOT NULL DEFAULT 0,
			genres TEXT NOT NULL DEFAULT '',
			genre_ids TEXT NOT NULL DEFAULT '',
			original_language TEXT NOT NULL DEFAULT '',
			origin_country TEXT NOT NULL DEFAULT '',
			keywords TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS genres TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS genre_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS original_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS origin_country TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS keywords TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS tv_episodes (
			id TEXT PRIMARY KEY,
			show_tmdb_id INT NOT NULL REFERENCES tv_shows(tmdb_id) ON DELETE CASCADE,
			tmdb_id INT NOT NULL,
			season INT NOT NULL,
			episode INT NOT NULL,
			title TEXT NOT NULL,
			air_date TEXT NOT NULL DEFAULT '',
			released BOOLEAN NOT NULL DEFAULT true,
			overview TEXT NOT NULL DEFAULT '',
			runtime INT NOT NULL DEFAULT 0,
			still_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(show_tmdb_id, season, episode)
		)`,
		`ALTER TABLE tv_episodes ADD COLUMN IF NOT EXISTS released BOOLEAN NOT NULL DEFAULT true`,
		`UPDATE tv_episodes SET released = CASE WHEN air_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN air_date::date <= CURRENT_DATE ELSE false END`,
		`CREATE TABLE IF NOT EXISTS collections (
			tmdb_id INT PRIMARY KEY,
			name TEXT NOT NULL,
			overview TEXT NOT NULL DEFAULT '',
			movie_count INT NOT NULL DEFAULT 0,
			unreleased_count INT NOT NULL DEFAULT 0,
			local_count INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'incomplete',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE collections ADD COLUMN IF NOT EXISTS unreleased_count INT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS collection_movies (
			collection_id INT NOT NULL REFERENCES collections(tmdb_id) ON DELETE CASCADE,
			movie_tmdb_id INT NOT NULL,
			title TEXT NOT NULL,
			release_date TEXT NOT NULL DEFAULT '',
			released BOOLEAN NOT NULL DEFAULT true,
			sort_order INT NOT NULL DEFAULT 0,
			PRIMARY KEY(collection_id, movie_tmdb_id)
		)`,
		`ALTER TABLE collection_movies ADD COLUMN IF NOT EXISTS released BOOLEAN NOT NULL DEFAULT true`,
		`UPDATE collection_movies SET released = CASE WHEN release_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN release_date::date <= CURRENT_DATE ELSE false END`,
		`UPDATE collections c SET
			movie_count = COALESCE((SELECT COUNT(*)::INT FROM collection_movies cm WHERE cm.collection_id=c.tmdb_id AND cm.released=true), 0),
			unreleased_count = COALESCE((SELECT COUNT(*)::INT FROM collection_movies cm WHERE cm.collection_id=c.tmdb_id AND cm.released=false), 0)`,
		`CREATE TABLE IF NOT EXISTS curated_collections (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			overview TEXT NOT NULL DEFAULT '',
			source_url TEXT NOT NULL DEFAULT '',
			item_count INT NOT NULL DEFAULT 0,
			resolved_count INT NOT NULL DEFAULT 0,
			local_count INT NOT NULL DEFAULT 0,
			missing_count INT NOT NULL DEFAULT 0,
			unresolved_count INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'incomplete',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			refreshed_at TIMESTAMPTZ,
			last_refresh_error TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS curated_collection_movies (
			list_id TEXT NOT NULL REFERENCES curated_collections(id) ON DELETE CASCADE,
			rank INT NOT NULL DEFAULT 0,
			douban_id TEXT NOT NULL,
			imdb_id TEXT NOT NULL DEFAULT '',
			movie_tmdb_id INT NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			original_title TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			release_date TEXT NOT NULL DEFAULT '',
			rating TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			source_url TEXT NOT NULL DEFAULT '',
			match_status TEXT NOT NULL DEFAULT 'pending',
			error_message TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(list_id, douban_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_curated_collection_movies_list_rank ON curated_collection_movies(list_id, rank)`,
		`CREATE INDEX IF NOT EXISTS idx_curated_collection_movies_tmdb ON curated_collection_movies(movie_tmdb_id)`,
		`INSERT INTO curated_collections (id, source, name, overview, source_url, item_count, status)
			VALUES ('douban_top250', 'douban', '豆瓣电影 Top250', '豆瓣电影 Top250 固定榜单，每天自动刷新并统计本地已有影片。', 'https://m.douban.com/subject_collection/movie_top250/', 0, 'incomplete')
			ON CONFLICT (id) DO NOTHING`,
		`UPDATE curated_collections SET source_url='https://m.douban.com/subject_collection/movie_top250/', updated_at=now()
			WHERE id='douban_top250' AND source_url IN ('', 'https://movie.douban.com/top250')`,
		`UPDATE curated_collections cc SET item_count=0, resolved_count=0, local_count=0, missing_count=0,
			unresolved_count=0, status='incomplete', updated_at=now()
			WHERE cc.id='douban_top250'
				AND NOT EXISTS (SELECT 1 FROM curated_collection_movies ccm WHERE ccm.list_id=cc.id)`,
		`CREATE TABLE IF NOT EXISTS media_matches (
			file_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
			target_type TEXT NOT NULL,
			movie_tmdb_id INT NOT NULL DEFAULT 0,
			show_tmdb_id INT NOT NULL DEFAULT 0,
			episode_id TEXT NOT NULL DEFAULT '',
			confidence NUMERIC NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS organize_tasks (
			id TEXT PRIMARY KEY,
			file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
			batch_id TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
			template_id TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL,
			target_path TEXT NOT NULL,
			task_status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			executed_at TIMESTAMPTZ
		)`,
		`ALTER TABLE organize_tasks ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`CREATE INDEX IF NOT EXISTS idx_organize_tasks_log_time ON organize_tasks((COALESCE(executed_at, created_at)) DESC)`,
		`CREATE TABLE IF NOT EXISTS operation_histories (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL DEFAULT '',
			file_id TEXT NOT NULL DEFAULT '',
			batch_id TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL DEFAULT '',
			target_path TEXT NOT NULL DEFAULT '',
			operation_name TEXT NOT NULL,
			operation_status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operation_histories_created ON operation_histories(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS collection_completion_histories (
			id TEXT PRIMARY KEY,
			collection_id INT NOT NULL,
			collection_tmdb_id INT NOT NULL,
			collection_name TEXT NOT NULL,
			source_path TEXT NOT NULL,
			target_path TEXT NOT NULL,
			movie_count INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_completion_histories_created ON collection_completion_histories(created_at DESC)`,
		`UPDATE batches b SET
			total=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id), 0),
			done=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='done'), 0),
			failed=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='failed'), 0),
			incomplete_collection=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='incomplete_collection'), 0)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Seed(ctx context.Context, dirs models.DirectoryConfig, settings models.SystemSettings, templates []models.NamingTemplate) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO system_directories (id, incoming_path, staging_path, failed_path, incomplete_collections_path)
		VALUES (1, $1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		dirs.IncomingPath, dirs.StagingPath, dirs.FailedPath, dirs.IncompleteCollectionsPath); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO system_settings (id, tmdb_api_key, network_proxy, classification_yaml,
		ai_filename_enabled, ai_filename_force, ai_base_url, ai_api_key, ai_model, ai_filename_prompt,
		clouddrive_address, clouddrive_username, clouddrive_password, clouddrive_token,
		clouddrive_root_path, clouddrive_staging_path, clouddrive_failed_path, clouddrive_incomplete_path)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) ON CONFLICT (id) DO NOTHING`,
		settings.TMDBAPIKey, settings.NetworkProxy, settings.ClassificationYAML,
		settings.AIFilenameEnabled, settings.AIFilenameForce, settings.AIBaseURL, settings.AIAPIKey, settings.AIModel, settings.AIFilenamePrompt,
		settings.CloudDriveAddress, settings.CloudDriveUsername, settings.CloudDrivePassword, settings.CloudDriveToken,
		settings.CloudDriveRootPath, settings.CloudDriveStagingPath, settings.CloudDriveFailedPath, settings.CloudDriveIncompletePath); err != nil {
		return err
	}
	if settings.ClassificationYAML != "" {
		if _, err := s.db.Exec(ctx, `UPDATE system_settings SET classification_yaml=$1 WHERE id=1 AND classification_yaml=''`, settings.ClassificationYAML); err != nil {
			return err
		}
	}
	for _, template := range templates {
		if _, err := s.db.Exec(ctx, `INSERT INTO naming_templates (template_type, name, template, enabled)
			VALUES ($1, $2, $3, $4) ON CONFLICT (template_type) DO NOTHING`,
			template.TemplateType, template.Name, template.Template, template.Enabled); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Settings(ctx context.Context) (models.SystemSettings, error) {
	var settings models.SystemSettings
	err := s.db.QueryRow(ctx, `SELECT tmdb_api_key, network_proxy, classification_yaml,
		ai_filename_enabled, ai_filename_force, ai_base_url, ai_api_key, ai_model, ai_filename_prompt,
		clouddrive_address, clouddrive_username, clouddrive_password, clouddrive_token,
		clouddrive_root_path, clouddrive_staging_path, clouddrive_failed_path, clouddrive_incomplete_path, updated_at
		FROM system_settings WHERE id=1`).
		Scan(&settings.TMDBAPIKey, &settings.NetworkProxy, &settings.ClassificationYAML,
			&settings.AIFilenameEnabled, &settings.AIFilenameForce, &settings.AIBaseURL, &settings.AIAPIKey, &settings.AIModel, &settings.AIFilenamePrompt,
			&settings.CloudDriveAddress, &settings.CloudDriveUsername, &settings.CloudDrivePassword, &settings.CloudDriveToken,
			&settings.CloudDriveRootPath, &settings.CloudDriveStagingPath, &settings.CloudDriveFailedPath, &settings.CloudDriveIncompletePath, &settings.UpdatedAt)
	if err != nil {
		return settings, err
	}
	return normalizeSystemSettings(settings), nil
}

func (s *Store) SaveSettings(ctx context.Context, settings models.SystemSettings) (models.SystemSettings, error) {
	settings = normalizeSystemSettings(settings)
	_, err := s.db.Exec(ctx, `INSERT INTO system_settings (id, tmdb_api_key, network_proxy, classification_yaml,
			ai_filename_enabled, ai_filename_force, ai_base_url, ai_api_key, ai_model, ai_filename_prompt)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET tmdb_api_key=$1, network_proxy=$2, classification_yaml=$3,
			ai_filename_enabled=$4, ai_filename_force=$5, ai_base_url=$6, ai_api_key=$7, ai_model=$8,
			ai_filename_prompt=$9, updated_at=now()`,
		settings.TMDBAPIKey, settings.NetworkProxy, settings.ClassificationYAML,
		settings.AIFilenameEnabled, settings.AIFilenameForce, settings.AIBaseURL, settings.AIAPIKey, settings.AIModel, settings.AIFilenamePrompt)
	if err != nil {
		return models.SystemSettings{}, err
	}
	return s.Settings(ctx)
}

func (s *Store) SaveClassification(ctx context.Context, yaml string) (models.SystemSettings, error) {
	current, err := s.Settings(ctx)
	if err != nil {
		return models.SystemSettings{}, err
	}
	current.ClassificationYAML = yaml
	return s.SaveSettings(ctx, current)
}

func normalizeSystemSettings(settings models.SystemSettings) models.SystemSettings {
	settings.AIBaseURL = strings.TrimRight(strings.TrimSpace(settings.AIBaseURL), "/")
	if settings.AIBaseURL == "" {
		settings.AIBaseURL = "https://api.openai.com/v1"
	}
	settings.AIModel = strings.TrimSpace(settings.AIModel)
	if settings.AIModel == "" {
		settings.AIModel = "gpt-5.5"
	}
	settings.AIFilenamePrompt = strings.TrimSpace(settings.AIFilenamePrompt)
	if settings.AIFilenamePrompt == "" {
		settings.AIFilenamePrompt = config.DefaultAIFilenamePrompt
	}
	return settings
}

func (s *Store) CloudDriveSettings(ctx context.Context) (models.CloudDriveSettings, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return models.CloudDriveSettings{}, err
	}
	return cloudDriveFromSystem(settings), nil
}

func (s *Store) SaveCloudDriveSettings(ctx context.Context, settings models.CloudDriveSettings) (models.CloudDriveSettings, error) {
	_, err := s.db.Exec(ctx, `UPDATE system_settings SET clouddrive_address=$1,
		clouddrive_username=$2, clouddrive_password=$3, clouddrive_token=$4, clouddrive_root_path=$5,
		clouddrive_staging_path=$6, clouddrive_failed_path=$7, clouddrive_incomplete_path=$8, updated_at=now()
		WHERE id=1`,
		settings.Address, settings.Username, settings.Password, settings.Token, settings.RootPath,
		settings.StagingPath, settings.FailedPath, settings.IncompleteCollectionsPath)
	if err != nil {
		return models.CloudDriveSettings{}, err
	}
	return s.CloudDriveSettings(ctx)
}

func cloudDriveFromSystem(settings models.SystemSettings) models.CloudDriveSettings {
	return models.CloudDriveSettings{
		Address:                   settings.CloudDriveAddress,
		Username:                  settings.CloudDriveUsername,
		Password:                  settings.CloudDrivePassword,
		Token:                     settings.CloudDriveToken,
		RootPath:                  settings.CloudDriveRootPath,
		StagingPath:               settings.CloudDriveStagingPath,
		FailedPath:                settings.CloudDriveFailedPath,
		IncompleteCollectionsPath: settings.CloudDriveIncompletePath,
		UpdatedAt:                 settings.UpdatedAt,
	}
}

func (s *Store) P115Settings(ctx context.Context) (models.P115Settings, error) {
	var settings models.P115Settings
	var openTokenRefreshedAt sql.NullTime
	err := s.db.QueryRow(ctx, `SELECT enabled, auth_mode, app_id, app_secret, access_token, refresh_token, cookies, cookie_login_app,
		strm_output_path, public_base_url, direct_url_ttl_seconds, user_agent_mode, fixed_user_agent, library_cid,
		delete_missing_strm, stale_before_delete, keep_deleted_days, refresh_emby_after_sync, sync_cron_enabled, sync_interval_minutes,
		emby_upstream_url, emby_public_url, emby_proxy_port, emby_proxy_base_path, emby_api_key, open_token_refreshed_at, updated_at
		FROM p115_settings WHERE id=1`).
		Scan(&settings.Enabled, &settings.AuthMode, &settings.AppID, &settings.AppSecret, &settings.AccessToken, &settings.RefreshToken, &settings.Cookies, &settings.CookieLoginApp,
			&settings.STRMOutputPath, &settings.PublicBaseURL, &settings.DirectURLTTLSeconds, &settings.UserAgentMode, &settings.FixedUserAgent, &settings.LibraryCID,
			&settings.DeleteMissingSTRM, &settings.StaleBeforeDelete, &settings.KeepDeletedDays, &settings.RefreshEmbyAfterSync, &settings.SyncCronEnabled, &settings.SyncIntervalMinutes,
			&settings.EmbyUpstreamURL, &settings.EmbyPublicURL, &settings.EmbyProxyPort, &settings.EmbyProxyBasePath, &settings.EmbyAPIKey, &openTokenRefreshedAt, &settings.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		settings = models.P115Settings{
			Enabled:             true,
			AuthMode:            "cookies",
			CookieLoginApp:      "wechatmini",
			STRMOutputPath:      "/data/Curio/strm",
			DirectURLTTLSeconds: 3000,
			UserAgentMode:       "inherit",
			DeleteMissingSTRM:   true,
			KeepDeletedDays:     7,
			SyncIntervalMinutes: 60,
			EmbyProxyPort:       8097,
			EmbyProxyBasePath:   "/emby",
		}
		if _, saveErr := s.SaveP115Settings(ctx, settings); saveErr != nil {
			return models.P115Settings{}, saveErr
		}
		return s.P115Settings(ctx)
	}
	if err != nil {
		return settings, err
	}
	if openTokenRefreshedAt.Valid {
		settings.OpenTokenRefreshedAt = &openTokenRefreshedAt.Time
	}
	return settings, nil
}

func (s *Store) SaveP115Settings(ctx context.Context, settings models.P115Settings) (models.P115Settings, error) {
	_, err := s.db.Exec(ctx, `INSERT INTO p115_settings (
		id, enabled, auth_mode, app_id, app_secret, access_token, refresh_token, cookies, cookie_login_app,
		strm_output_path, public_base_url, direct_url_ttl_seconds, user_agent_mode, fixed_user_agent, library_cid,
		delete_missing_strm, stale_before_delete, keep_deleted_days, refresh_emby_after_sync, sync_cron_enabled, sync_interval_minutes,
		emby_upstream_url, emby_public_url, emby_proxy_port, emby_proxy_base_path, emby_api_key, open_token_refreshed_at
	) VALUES (1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
	ON CONFLICT (id) DO UPDATE SET
		enabled=$1, auth_mode=$2, app_id=$3, app_secret=$4, access_token=$5, refresh_token=$6, cookies=$7, cookie_login_app=$8,
		strm_output_path=$9, public_base_url=$10, direct_url_ttl_seconds=$11, user_agent_mode=$12, fixed_user_agent=$13, library_cid=$14,
		delete_missing_strm=$15, stale_before_delete=$16, keep_deleted_days=$17, refresh_emby_after_sync=$18, sync_cron_enabled=$19, sync_interval_minutes=$20,
		emby_upstream_url=$21, emby_public_url=$22, emby_proxy_port=$23, emby_proxy_base_path=$24, emby_api_key=$25, open_token_refreshed_at=$26, updated_at=now()`,
		settings.Enabled, settings.AuthMode, settings.AppID, settings.AppSecret, settings.AccessToken, settings.RefreshToken, settings.Cookies, settings.CookieLoginApp,
		settings.STRMOutputPath, settings.PublicBaseURL, settings.DirectURLTTLSeconds, settings.UserAgentMode, settings.FixedUserAgent, settings.LibraryCID,
		settings.DeleteMissingSTRM, settings.StaleBeforeDelete, settings.KeepDeletedDays, settings.RefreshEmbyAfterSync, settings.SyncCronEnabled, settings.SyncIntervalMinutes,
		settings.EmbyUpstreamURL, settings.EmbyPublicURL, settings.EmbyProxyPort, settings.EmbyProxyBasePath, settings.EmbyAPIKey, settings.OpenTokenRefreshedAt)
	if err != nil {
		return models.P115Settings{}, err
	}
	return s.P115Settings(ctx)
}

func (s *Store) UpsertSTRMLink(ctx context.Context, link models.STRMLink) error {
	_, err := s.db.Exec(ctx, `INSERT INTO strm_links (
			id, provider, library_cid, library_name, library_type, relative_path, remote_path, remote_file_id, pickcode, sha1, size,
			strm_path, play_path, source_tree_hash, tree_version, resolve_status, status, error_code, error_message, generated_at, resolved_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (provider, library_cid, relative_path) DO UPDATE SET
			library_name=$4, library_type=$5, remote_path=$7,
			remote_file_id=COALESCE(NULLIF($8, ''), strm_links.remote_file_id),
			pickcode=COALESCE(NULLIF($9, ''), strm_links.pickcode),
			sha1=COALESCE(NULLIF($10, ''), strm_links.sha1),
			size=CASE WHEN $11 > 0 THEN $11 ELSE strm_links.size END,
			strm_path=$12, play_path=$13, source_tree_hash=$14, tree_version=$15,
			resolve_status=CASE WHEN NULLIF($9, '') IS NULL THEN strm_links.resolve_status ELSE $16 END,
			status=$17, error_code=$18, error_message=$19, updated_at=now()`,
		link.ID, link.Provider, link.LibraryCID, link.LibraryName, link.LibraryType, link.RelativePath, link.RemotePath, link.RemoteFileID, link.PickCode, link.SHA1, link.Size,
		link.STRMPath, link.PlayPath, link.SourceTreeHash, link.TreeVersion, link.ResolveStatus, link.Status, link.ErrorCode, link.ErrorMessage, link.GeneratedAt, link.ResolvedAt)
	return err
}

func (s *Store) STRMLink(ctx context.Context, id string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links WHERE id=$1`, id)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByPath(ctx context.Context, path string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links WHERE strm_path=$1 OR play_path=$1 ORDER BY updated_at DESC LIMIT 1`, path)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByPlayRoute(ctx context.Context, provider, route string, playPaths []string) (models.STRMLink, error) {
	route = strings.Trim(strings.ReplaceAll(route, "\\", "/"), "/")
	paths := make([]string, 0, len(playPaths)+1)
	seen := map[string]struct{}{}
	for _, value := range append(playPaths, "/play/115/"+route) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	if route == "" || len(paths) == 0 {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	playSuffix := "/play/115/" + route
	relativeSuffix := "/" + route
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links
		WHERE provider=$1 AND status=$2 AND (
			play_path=ANY($3)
			OR right(play_path, length($4))=$4
			OR relative_path=$5
			OR right(relative_path, length($6))=$6
		)
		ORDER BY CASE
			WHEN play_path=ANY($3) THEN 0
			WHEN right(play_path, length($4))=$4 THEN 1
			WHEN relative_path=$5 THEN 2
			ELSE 3
		END, updated_at DESC
		LIMIT 1`, provider, models.STRMStatusGenerated, paths, playSuffix, route, relativeSuffix)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByEmbyItem(ctx context.Context, serverID, itemID string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT sl.id, sl.provider, sl.library_cid, sl.library_name, sl.library_type, sl.relative_path, sl.remote_path,
		sl.remote_file_id, sl.pickcode, sl.sha1, sl.size, sl.strm_path, sl.play_path, sl.source_tree_hash, sl.tree_version, sl.resolve_status, sl.status,
		sl.error_code, sl.error_message, sl.media_streams_json, sl.media_duration_ticks, sl.media_probed_at, sl.media_probe_error, sl.generated_at, sl.resolved_at, sl.updated_at
		FROM emby_strm_items ei
		JOIN strm_links sl ON sl.id=ei.strm_link_id
		WHERE ei.emby_server_id=$1 AND ei.emby_item_id=$2 AND ei.status='active'
		ORDER BY ei.last_seen_at DESC LIMIT 1`, serverID, itemID)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) NextSTRMLinks(ctx context.Context, link models.STRMLink, limit int) ([]models.STRMLink, error) {
	if limit <= 0 {
		return nil, nil
	}
	relativePath := strings.Trim(strings.ReplaceAll(link.RelativePath, "\\", "/"), "/")
	if relativePath == "" {
		return nil, nil
	}
	dir := path.Dir(relativePath)
	if dir == "." || dir == "/" {
		return nil, nil
	}
	prefix := strings.TrimRight(dir, "/") + "/"
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links
		WHERE provider=$1 AND library_cid=$2 AND status=$3
			AND relative_path>$4
			AND left(relative_path, length($5))=$5
		ORDER BY relative_path ASC
		LIMIT $6`, link.Provider, link.LibraryCID, models.STRMStatusGenerated, relativePath, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]models.STRMLink, 0, limit)
	for rows.Next() {
		next, err := scanSTRMLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, next)
	}
	return links, rows.Err()
}

func (s *Store) ActiveSTRMLinksByLibrary(ctx context.Context, provider, libraryCID string) ([]models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links WHERE provider=$1 AND library_cid=$2 AND status IN ($3,$4)`, provider, libraryCID, models.STRMStatusGenerated, models.STRMStatusStale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]models.STRMLink, 0)
	for rows.Next() {
		link, err := scanSTRMLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) STRMLinksByStatuses(ctx context.Context, statuses []string) ([]models.STRMLink, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, media_streams_json, media_duration_ticks, media_probed_at, media_probe_error, generated_at, resolved_at, updated_at
		FROM strm_links WHERE status=ANY($1)`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]models.STRMLink, 0)
	for rows.Next() {
		link, err := scanSTRMLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) MarkSTRMLinkStatus(ctx context.Context, id, status, resolveStatus, code, message string) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET status=$2, resolve_status=$3, error_code=$4, error_message=$5, updated_at=now() WHERE id=$1`,
		id, status, resolveStatus, code, message)
	return err
}

func (s *Store) UpdateSTRMLinkResolved(ctx context.Context, id, remoteFileID, pickcode, sha1 string, size int64) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET remote_file_id=$2, pickcode=$3, sha1=$4, size=$5,
		resolve_status=$6, error_code='', error_message='', resolved_at=now(), updated_at=now() WHERE id=$1`,
		id, remoteFileID, pickcode, sha1, size, models.STRMResolveResolved)
	return err
}

func (s *Store) UpdateSTRMLinkPlayPath(ctx context.Context, id, playPath string) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET play_path=$2, updated_at=now() WHERE id=$1`, id, playPath)
	return err
}

func (s *Store) UpdateSTRMLinkMediaStreams(ctx context.Context, id, streamsJSON string, durationTicks int64, probeError string) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET
		media_streams_json=CASE WHEN $4 = '' THEN $2 ELSE media_streams_json END,
		media_duration_ticks=CASE WHEN $4 = '' THEN $3 ELSE media_duration_ticks END,
		media_probe_error=$4, media_probed_at=now(), updated_at=now() WHERE id=$1`,
		id, streamsJSON, durationTicks, probeError)
	return err
}

func (s *Store) CreateP115SyncRun(ctx context.Context, run models.P115SyncRun) error {
	if run.Status == "" {
		run.Status = models.P115SyncStatusRunning
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	_, err := s.db.Exec(ctx, `INSERT INTO p115_sync_runs
		(id, trigger, status, mode, tree_version, exported, generated, restored, updated_count, deleted_count, skipped, failed, error_message, event_summary, started_at, duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		run.ID, run.Trigger, run.Status, run.Mode, run.TreeVersion, run.Exported, run.Generated, run.Restored, run.Updated, run.Deleted, run.Skipped, run.Failed, run.ErrorMessage, run.EventSummary, run.StartedAt, run.DurationMS)
	return err
}

func (s *Store) FinishP115SyncRun(ctx context.Context, run models.P115SyncRun) error {
	_, err := s.db.Exec(ctx, `UPDATE p115_sync_runs SET
		status=$2, mode=$3, tree_version=$4, exported=$5, generated=$6, restored=$7, updated_count=$8,
		deleted_count=$9, skipped=$10, failed=$11, error_message=$12, event_summary=$13, ended_at=$14, duration_ms=$15
		WHERE id=$1`,
		run.ID, run.Status, run.Mode, run.TreeVersion, run.Exported, run.Generated, run.Restored, run.Updated,
		run.Deleted, run.Skipped, run.Failed, run.ErrorMessage, run.EventSummary, run.EndedAt, run.DurationMS)
	return err
}

func (s *Store) P115SyncRuns(ctx context.Context, limit int) ([]models.P115SyncRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(ctx, `SELECT id, trigger, status, mode, tree_version, exported, generated, restored, updated_count,
		deleted_count, skipped, failed, error_message, event_summary, started_at, ended_at, duration_ms
		FROM p115_sync_runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]models.P115SyncRun, 0)
	for rows.Next() {
		run, err := scanP115SyncRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) LatestP115SyncRun(ctx context.Context, triggers []string) (models.P115SyncRun, bool, error) {
	if len(triggers) == 0 {
		return models.P115SyncRun{}, false, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, trigger, status, mode, tree_version, exported, generated, restored, updated_count,
		deleted_count, skipped, failed, error_message, event_summary, started_at, ended_at, duration_ms
		FROM p115_sync_runs WHERE trigger=ANY($1) ORDER BY started_at DESC LIMIT 1`, triggers)
	if err != nil {
		return models.P115SyncRun{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.P115SyncRun{}, false, rows.Err()
	}
	run, err := scanP115SyncRun(rows)
	return run, err == nil, err
}

func (s *Store) AddAIFilenameLog(ctx context.Context, entry models.AIFilenameLog) error {
	_, err := s.db.Exec(ctx, `INSERT INTO ai_filename_logs
		(id, batch_id, file_id, file_path, file_name, source, status, model, base_url, proxy_url, response_format,
			request_json, response_json, parsed_json, http_status, duration_ms, attempt, media_type, title, year, season, episode,
			confidence, needs_review, reason, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`,
		entry.ID, entry.BatchID, entry.FileID, entry.FilePath, entry.FileName, entry.Source, entry.Status, entry.Model, entry.BaseURL,
		entry.ProxyURL, entry.ResponseFormat, entry.RequestJSON, entry.ResponseJSON, entry.ParsedJSON, entry.HTTPStatus, entry.DurationMS,
		entry.Attempt, entry.MediaType, entry.Title, entry.Year, entry.Season, entry.Episode, entry.Confidence, entry.NeedsReview,
		entry.Reason, entry.ErrorMessage)
	return err
}

func (s *Store) AttachAIFilenameLogFile(ctx context.Context, batchID, filePath, fileID string) error {
	if batchID == "" || filePath == "" || fileID == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `UPDATE ai_filename_logs SET file_id=$3
		WHERE batch_id=$1 AND file_path=$2 AND file_id=''`, batchID, filePath, fileID)
	return err
}

func (s *Store) LogEntries(ctx context.Context, logType string, limit, offset int) (models.LogPage, error) {
	logType = normalizeLogType(logType)
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	fragments := logQueryFragments(logType, false)
	if len(fragments) == 0 {
		return models.LogPage{Items: []models.LogEntry{}, Limit: limit, Offset: offset, Type: logType}, nil
	}
	union := strings.Join(fragments, "\nUNION ALL\n")
	var total int
	if err := s.db.QueryRow(ctx, `WITH logs AS (`+union+`) SELECT count(*) FROM logs`).Scan(&total); err != nil {
		return models.LogPage{}, err
	}
	rows, err := s.db.Query(ctx, `WITH logs AS (`+union+`) SELECT id, type, source, status, message, detail,
		batch_id, file_id, file_name, path, model, base_url, proxy_url, response_format,
		request_json, response_json, parsed_json, http_status, duration_ms, error_message, created_at
		FROM logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return models.LogPage{}, err
	}
	entries := make([]models.LogEntry, 0, limit)
	if err := appendLogRows(rows, &entries); err != nil {
		return models.LogPage{}, err
	}
	return models.LogPage{Items: entries, Total: total, Limit: limit, Offset: offset, Type: logType}, nil
}

func (s *Store) LogEntry(ctx context.Context, id string) (models.LogEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return models.LogEntry{}, pgx.ErrNoRows
	}
	fragments := logQueryFragments("all", true)
	union := strings.Join(fragments, "\nUNION ALL\n")
	rows, err := s.db.Query(ctx, `WITH logs AS (`+union+`) SELECT id, type, source, status, message, detail,
		batch_id, file_id, file_name, path, model, base_url, proxy_url, response_format,
		request_json, response_json, parsed_json, http_status, duration_ms, error_message, created_at
		FROM logs WHERE id=$1 ORDER BY created_at DESC LIMIT 1`, id)
	if err != nil {
		return models.LogEntry{}, err
	}
	entries := []models.LogEntry{}
	if err := appendLogRows(rows, &entries); err != nil {
		return models.LogEntry{}, err
	}
	if len(entries) == 0 {
		return models.LogEntry{}, pgx.ErrNoRows
	}
	return entries[0], nil
}

func (s *Store) ReplaceP115Snapshot(ctx context.Context, libraryCID, treeVersion string, items []models.P115TreeSnapshotItem) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_tree_snapshots WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, item := range items {
		contentKey := item.LibraryCID + ":" + item.RelativePath
		if _, err := tx.Exec(ctx, `INSERT INTO p115_tree_snapshots
			(library_cid, content_key, tree_version, relative_path, name, remote_file_id, parent_file_id, pickcode, sha1, size, extension, depth, is_directory, is_media, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			libraryCID, contentKey, treeVersion, item.RelativePath, item.Name, item.RemoteFileID, item.ParentFileID, item.PickCode, item.SHA1, item.Size, item.Extension, item.Depth, item.IsDirectory, item.IsMedia, item.SourceTreeHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) P115Snapshot(ctx context.Context, libraryCID string) ([]models.P115TreeSnapshotItem, string, error) {
	rows, err := s.db.Query(ctx, `SELECT library_cid, tree_version, relative_path, name, remote_file_id, parent_file_id, pickcode, sha1, size, extension, depth, is_directory, is_media, source_tree_hash
		FROM p115_tree_snapshots WHERE library_cid=$1 ORDER BY relative_path`, libraryCID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]models.P115TreeSnapshotItem, 0)
	version := ""
	for rows.Next() {
		var item models.P115TreeSnapshotItem
		if err := rows.Scan(&item.LibraryCID, &item.TreeVersion, &item.RelativePath, &item.Name, &item.RemoteFileID, &item.ParentFileID, &item.PickCode, &item.SHA1, &item.Size, &item.Extension, &item.Depth, &item.IsDirectory, &item.IsMedia, &item.SourceTreeHash); err != nil {
			return nil, "", err
		}
		if version == "" {
			version = item.TreeVersion
		}
		items = append(items, item)
	}
	return items, version, rows.Err()
}

func (s *Store) ReplaceP115Nodes(ctx context.Context, libraryCID, treeVersion string, nodes []models.P115Node) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_nodes WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.RemoteFileID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO p115_nodes
			(library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			libraryCID, node.RemoteFileID, node.ParentFileID, treeVersion, node.RelativePath, node.Name, node.PickCode, node.SHA1, node.Size, node.IsDirectory, node.IsMedia, node.IsAlive, node.SourceTreeHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReplaceP115NodesAndCursor(ctx context.Context, libraryCID, treeVersion string, nodes []models.P115Node, cursor models.P115EventCursor) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_nodes WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.RemoteFileID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO p115_nodes
			(library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			libraryCID, node.RemoteFileID, node.ParentFileID, treeVersion, node.RelativePath, node.Name, node.PickCode, node.SHA1, node.Size, node.IsDirectory, node.IsMedia, node.IsAlive, node.SourceTreeHash); err != nil {
			return err
		}
	}
	if cursor.LibraryCID == "" {
		cursor.LibraryCID = libraryCID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO p115_event_cursors
		(library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (library_cid) DO UPDATE SET
			last_event_id=$2, last_event_time=$3, last_sync_status=$4, last_sync_error=$5, updated_at=now()`,
		cursor.LibraryCID, cursor.LastEventID, cursor.LastEventTime, cursor.LastSyncStatus, cursor.LastSyncError); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) P115Nodes(ctx context.Context, libraryCID string, aliveOnly bool) ([]models.P115Node, string, error) {
	query := `SELECT library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash, updated_at
		FROM p115_nodes WHERE library_cid=$1`
	if aliveOnly {
		query += ` AND is_alive`
	}
	query += ` ORDER BY relative_path`
	rows, err := s.db.Query(ctx, query, libraryCID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	nodes := make([]models.P115Node, 0)
	version := ""
	for rows.Next() {
		var node models.P115Node
		if err := rows.Scan(&node.LibraryCID, &node.RemoteFileID, &node.ParentFileID, &node.TreeVersion, &node.RelativePath, &node.Name, &node.PickCode, &node.SHA1, &node.Size, &node.IsDirectory, &node.IsMedia, &node.IsAlive, &node.SourceTreeHash, &node.UpdatedAt); err != nil {
			return nil, "", err
		}
		if version == "" {
			version = node.TreeVersion
		}
		nodes = append(nodes, node)
	}
	return nodes, version, rows.Err()
}

func (s *Store) P115NodeCount(ctx context.Context, libraryCID string) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM p115_nodes WHERE library_cid=$1 AND is_alive`, libraryCID).Scan(&count)
	return count, err
}

func (s *Store) P115EventCursor(ctx context.Context, libraryCID string) (models.P115EventCursor, error) {
	var cursor models.P115EventCursor
	err := s.db.QueryRow(ctx, `SELECT library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error, updated_at
		FROM p115_event_cursors WHERE library_cid=$1`, libraryCID).
		Scan(&cursor.LibraryCID, &cursor.LastEventID, &cursor.LastEventTime, &cursor.LastSyncStatus, &cursor.LastSyncError, &cursor.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		cursor.LibraryCID = libraryCID
		return cursor, nil
	}
	return cursor, err
}

func (s *Store) SaveP115EventCursor(ctx context.Context, cursor models.P115EventCursor) error {
	_, err := s.db.Exec(ctx, `INSERT INTO p115_event_cursors
		(library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (library_cid) DO UPDATE SET
			last_event_id=$2, last_event_time=$3, last_sync_status=$4, last_sync_error=$5, updated_at=now()`,
		cursor.LibraryCID, cursor.LastEventID, cursor.LastEventTime, cursor.LastSyncStatus, cursor.LastSyncError)
	return err
}

func (s *Store) UpsertEmbySTRMItem(ctx context.Context, item models.EmbySTRMItem) error {
	_, err := s.db.Exec(ctx, `INSERT INTO emby_strm_items (id, emby_server_id, emby_item_id, strm_link_id, strm_path, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (emby_server_id, emby_item_id) DO UPDATE SET
			strm_link_id=$4, strm_path=$5, status=$6, last_seen_at=now(), updated_at=now()`,
		item.ID, item.EmbyServerID, item.EmbyItemID, item.STRMLinkID, item.STRMPath, item.Status)
	return err
}

func (s *Store) UpsertEmbyPlaybackProgress(ctx context.Context, progress models.EmbyPlaybackProgress) error {
	progress.EmbyServerID = strings.TrimSpace(progress.EmbyServerID)
	if progress.EmbyServerID == "" {
		progress.EmbyServerID = "default"
	}
	progress.UserID = strings.TrimSpace(progress.UserID)
	progress.EmbyItemID = strings.TrimSpace(progress.EmbyItemID)
	if progress.UserID == "" || progress.EmbyItemID == "" {
		return nil
	}
	if strings.TrimSpace(progress.ID) == "" {
		progress.ID = progress.EmbyServerID + ":" + progress.UserID + ":" + progress.EmbyItemID
	}
	_, err := s.db.Exec(ctx, `INSERT INTO emby_playback_progress
		(id, emby_server_id, user_id, emby_item_id, strm_link_id, position_ticks, duration_ticks, played, client, device, play_session_id, last_event)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (emby_server_id, user_id, emby_item_id) DO UPDATE SET
			strm_link_id=EXCLUDED.strm_link_id,
			position_ticks=EXCLUDED.position_ticks,
			duration_ticks=CASE
				WHEN EXCLUDED.duration_ticks > 0 THEN EXCLUDED.duration_ticks
				ELSE emby_playback_progress.duration_ticks
			END,
			played=EXCLUDED.played,
			client=EXCLUDED.client,
			device=EXCLUDED.device,
			play_session_id=EXCLUDED.play_session_id,
			last_event=EXCLUDED.last_event,
			cleared_at=NULL,
			updated_at=now()`,
		progress.ID, progress.EmbyServerID, progress.UserID, progress.EmbyItemID, progress.STRMLinkID,
		progress.PositionTicks, progress.DurationTicks, progress.Played, progress.Client, progress.Device,
		progress.PlaySessionID, progress.LastEvent)
	return err
}

func (s *Store) ClearEmbyPlaybackProgress(ctx context.Context, serverID, userID, itemID, event string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		serverID = "default"
	}
	userID = strings.TrimSpace(userID)
	itemID = strings.TrimSpace(itemID)
	if userID == "" || itemID == "" {
		return nil
	}
	event = strings.TrimSpace(event)
	if event == "" {
		event = "clear"
	}
	_, err := s.db.Exec(ctx, `UPDATE emby_playback_progress
		SET position_ticks=0, played=false, last_event=$4, cleared_at=now(), updated_at=now()
		WHERE emby_server_id=$1 AND user_id=$2 AND emby_item_id=$3`,
		serverID, userID, itemID, event)
	return err
}

func (s *Store) RecentEmbyPlaybackProgress(ctx context.Context, serverID, userID string, limit int) ([]models.EmbyPlaybackProgress, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		serverID = "default"
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.Query(ctx, `SELECT id, emby_server_id, user_id, emby_item_id, strm_link_id,
			position_ticks, duration_ticks, played, client, device, play_session_id, last_event, updated_at, cleared_at
		FROM emby_playback_progress
		WHERE emby_server_id=$1 AND user_id=$2 AND cleared_at IS NULL AND played=false AND position_ticks > 0
		ORDER BY updated_at DESC
		LIMIT $3`, serverID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	progresses := make([]models.EmbyPlaybackProgress, 0)
	for rows.Next() {
		var progress models.EmbyPlaybackProgress
		var clearedAt sql.NullTime
		if err := rows.Scan(&progress.ID, &progress.EmbyServerID, &progress.UserID, &progress.EmbyItemID, &progress.STRMLinkID,
			&progress.PositionTicks, &progress.DurationTicks, &progress.Played, &progress.Client, &progress.Device,
			&progress.PlaySessionID, &progress.LastEvent, &progress.UpdatedAt, &clearedAt); err != nil {
			return nil, err
		}
		if clearedAt.Valid {
			progress.ClearedAt = &clearedAt.Time
		}
		progresses = append(progresses, progress)
	}
	return progresses, rows.Err()
}

func (s *Store) Directories(ctx context.Context) (models.DirectoryConfig, error) {
	var dirs models.DirectoryConfig
	err := s.db.QueryRow(ctx, `SELECT incoming_path, staging_path, failed_path, incomplete_collections_path FROM system_directories WHERE id=1`).
		Scan(&dirs.IncomingPath, &dirs.StagingPath, &dirs.FailedPath, &dirs.IncompleteCollectionsPath)
	return dirs, err
}

func (s *Store) SaveDirectories(ctx context.Context, dirs models.DirectoryConfig) error {
	_, err := s.db.Exec(ctx, `INSERT INTO system_directories (id, incoming_path, staging_path, failed_path, incomplete_collections_path)
		VALUES (1, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET incoming_path=$1, staging_path=$2, failed_path=$3, incomplete_collections_path=$4, updated_at=now()`,
		dirs.IncomingPath, dirs.StagingPath, dirs.FailedPath, dirs.IncompleteCollectionsPath)
	return err
}

func (s *Store) Templates(ctx context.Context) ([]models.NamingTemplate, error) {
	rows, err := s.db.Query(ctx, `SELECT template_type, name, template, enabled, updated_at FROM naming_templates ORDER BY template_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := make([]models.NamingTemplate, 0)
	for rows.Next() {
		var template models.NamingTemplate
		if err := rows.Scan(&template.TemplateType, &template.Name, &template.Template, &template.Enabled, &template.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

func (s *Store) Template(ctx context.Context, templateType string) (models.NamingTemplate, error) {
	var template models.NamingTemplate
	err := s.db.QueryRow(ctx, `SELECT template_type, name, template, enabled, updated_at FROM naming_templates WHERE template_type=$1 AND enabled=true`, templateType).
		Scan(&template.TemplateType, &template.Name, &template.Template, &template.Enabled, &template.UpdatedAt)
	return template, err
}

func (s *Store) SaveTemplate(ctx context.Context, template models.NamingTemplate) error {
	_, err := s.db.Exec(ctx, `INSERT INTO naming_templates (template_type, name, template, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (template_type) DO UPDATE SET name=$2, template=$3, enabled=$4, updated_at=now()`,
		template.TemplateType, template.Name, template.Template, template.Enabled)
	return err
}

func (s *Store) CreateBatch(ctx context.Context, id, source string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO batches (id, source, status) VALUES ($1, $2, $3)`, id, source, models.BatchStatusQueued)
	return err
}

func (s *Store) SetBatchStatus(ctx context.Context, id, status string) error {
	var allowed []string
	switch status {
	case models.BatchStatusRunning:
		allowed = []string{models.BatchStatusQueued}
	case models.BatchStatusCancelling:
		allowed = []string{models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling}
	default:
		return fmt.Errorf("非法批次状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE batches SET status=$2 WHERE id=$1 AND ended_at IS NULL AND status=ANY($3)`, id, status, allowed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("批次 %s 无法流转到 %s", id, status)
	}
	return nil
}

func (s *Store) SetBatchTotal(ctx context.Context, id string, total int) error {
	_, err := s.db.Exec(ctx, `UPDATE batches SET total=$2 WHERE id=$1`, id, total)
	return err
}

func (s *Store) IncrementBatch(ctx context.Context, id, bucket string) error {
	column := map[string]string{
		models.StatusDone:                 "done",
		models.StatusFailed:               "failed",
		models.StatusIncompleteCollection: "incomplete_collection",
	}[bucket]
	if column == "" {
		return fmt.Errorf("未知批次计数字段 %s", bucket)
	}
	_, err := s.db.Exec(ctx, fmt.Sprintf(`UPDATE batches SET %s=%s+1 WHERE id=$1`, column, column), id)
	return err
}

func (s *Store) FinishBatch(ctx context.Context, id, status string) error {
	if !isTerminalBatchStatus(status) {
		return fmt.Errorf("非法批次结束状态 %s", status)
	}
	_, err := s.db.Exec(ctx, `UPDATE batches SET
		status=CASE WHEN status=$3 AND $2=$4 THEN $5 ELSE $2 END,
		ended_at=now()
		WHERE id=$1 AND ended_at IS NULL`,
		id, status, models.BatchStatusCancelling, models.BatchStatusComplete, models.BatchStatusCancelled)
	return err
}

func (s *Store) InterruptActiveBatches(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `UPDATE batches SET status=$1, ended_at=now()
		WHERE status IN ($2,$3,$4) AND ended_at IS NULL`,
		models.BatchStatusInterrupted, models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling)
	return err
}

func (s *Store) ActiveBatch(ctx context.Context) (models.Batch, bool, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := s.db.QueryRow(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at
		FROM batches WHERE status IN ($1,$2,$3) AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`,
		models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling).
		Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Batch{}, false, nil
	}
	if err != nil {
		return models.Batch{}, false, err
	}
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, true, nil
}

func (s *Store) Batch(ctx context.Context, id string) (models.Batch, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := s.db.QueryRow(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at FROM batches WHERE id=$1`, id).
		Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, err
}

func (s *Store) Batches(ctx context.Context, limit int) ([]models.Batch, error) {
	rows, err := s.db.Query(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at FROM batches ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := make([]models.Batch, 0)
	for rows.Next() {
		batch, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *Store) CreateMediaFile(ctx context.Context, file models.MediaFile) error {
	_, err := s.db.Exec(ctx, `INSERT INTO media_files
		(id, batch_id, original_path, current_path, original_file_name, extension, file_size, file_hash, hash_type, process_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		file.ID, file.BatchID, file.OriginalPath, file.CurrentPath, file.OriginalName, file.Extension, file.FileSize, file.FileHash, file.HashType, file.ProcessStatus)
	return err
}

func (s *Store) UpdateMediaStatus(ctx context.Context, id, status string) error {
	return s.updateMediaStatus(ctx, id, status, previousMediaStatuses(status)...)
}

func (s *Store) UpdateMediaParsed(ctx context.Context, id, parseTitle string, year, season, episode int, source string, technical models.MediaTechnicalInfo) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET parse_title=$2, parse_year=$3, season=$4, episode=$5,
		resolution=$6, source=$7, video_codec=$8, audio_codec=$9, audio_channels=$10, hdr_format=$11, process_status=$12, updated_at=now() WHERE id=$1 AND process_status=ANY($13)`,
		id, parseTitle, nullableInt(year), nullableInt(season), nullableInt(episode), technical.Resolution, source, technical.VideoCodec,
		technical.AudioCodec, technical.AudioChannels, technical.HDRFormat, models.StatusParsed,
		previousMediaStatuses(models.StatusParsed))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusParsed)
}

func (s *Store) UpdateMediaParsedTV(ctx context.Context, id string, year, season, episode int) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET parse_year=$2, season=$3, episode=$4, updated_at=now()
		WHERE id=$1 AND process_status=ANY($5)`,
		id, nullableInt(year), nullableInt(season), nullableInt(episode), []string{models.StatusParsed})
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "media file "+id+" failed to refresh parsed tv fields")
}

func (s *Store) UpdateMediaMatched(ctx context.Context, id, mediaType string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET media_type=$2, match_status='matched', process_status=$3, updated_at=now()
		WHERE id=$1 AND process_status=ANY($4)`,
		id, mediaType, models.StatusMatched, previousMediaStatuses(models.StatusMatched))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusMatched)
}

func (s *Store) UpdateMediaPlanned(ctx context.Context, id, targetPath string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET planned_target=$2, process_status=$3, updated_at=now()
		WHERE id=$1 AND process_status=ANY($4)`,
		id, targetPath, models.StatusPlanned, previousMediaStatuses(models.StatusPlanned))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusPlanned)
}

func (s *Store) IncrementMoveAttempt(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `UPDATE media_files SET move_attempts=move_attempts+1, updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateMediaFinal(ctx context.Context, id, currentPath, finalPath, finalName, status string) error {
	if status != models.StatusDone && status != models.StatusIncompleteCollection {
		return fmt.Errorf("非法媒体完成状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET current_path=$2, final_path=$3, final_file_name=$4, process_status=$5, last_verified_path=$3,
		error_code='', error_message='', updated_at=now() WHERE id=$1 AND process_status=ANY($6)`,
		id, currentPath, finalPath, finalName, status, previousMediaStatuses(status))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+status)
}

func (s *Store) UpdateMediaFailure(ctx context.Context, id, currentPath, failedPath, code, message string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET current_path=$2, final_path=$3, process_status=$4, error_code=$5,
		error_message=$6, updated_at=now()
		WHERE id=$1 AND process_status NOT IN ($7,$8,$9)`,
		id, currentPath, failedPath, models.StatusFailed, code, message,
		models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusFailed)
}

func (s *Store) MediaFile(ctx context.Context, id string) (models.MediaFile, error) {
	rows, err := s.db.Query(ctx, `SELECT id, batch_id, original_path, current_path, final_path, original_file_name, final_file_name,
		extension, file_size, file_hash, hash_type, media_type, process_status, match_status, parse_title,
		COALESCE(parse_year,0), COALESCE(season,0), COALESCE(episode,0), resolution, source, video_codec, audio_codec, audio_channels, hdr_format,
		planned_target, move_attempts, last_verified_path, error_code, error_message, created_at, updated_at FROM media_files WHERE id=$1`, id)
	if err != nil {
		return models.MediaFile{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return models.MediaFile{}, err
		}
		return models.MediaFile{}, pgx.ErrNoRows
	}
	return scanMediaFile(rows)
}

func (s *Store) DeleteMediaFile(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM media_files WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 不存在")
}

func (s *Store) DeleteMediaFiles(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("媒体文件 ID 不能为空")
	}
	rows, err := s.db.Query(ctx, `SELECT DISTINCT batch_id FROM media_files WHERE id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	batchIDs := make([]string, 0)
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return nil, err
		}
		batchIDs = append(batchIDs, batchID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	tag, err := s.db.Exec(ctx, `DELETE FROM media_files WHERE id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	return batchIDs, requireRows(tag.RowsAffected(), "没有可删除的媒体记录")
}

func (s *Store) UpdateMediaTechnical(ctx context.Context, id string, technical models.MediaTechnicalInfo) error {
	_, err := s.db.Exec(ctx, `UPDATE media_files SET resolution=$2, video_codec=$3, audio_codec=$4,
		audio_channels=$5, hdr_format=$6, updated_at=now() WHERE id=$1`,
		id, technical.Resolution, technical.VideoCodec, technical.AudioCodec, technical.AudioChannels, technical.HDRFormat)
	return err
}

func (s *Store) UpdateMediaRearchiveFinal(ctx context.Context, id, parseTitle string, year, season, episode int, source string, technical models.MediaTechnicalInfo, mediaType, targetPath, movedPath, finalName, status string) error {
	if status != models.StatusDone && status != models.StatusIncompleteCollection {
		return fmt.Errorf("非法媒体完成状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET parse_title=$2, parse_year=$3, season=$4, episode=$5, source=$6,
		resolution=$7, video_codec=$8, audio_codec=$9, audio_channels=$10, hdr_format=$11, media_type=$12, match_status='matched',
		process_status=$13, planned_target=$14, current_path=$15, final_path=$16, final_file_name=$17, last_verified_path=$16,
		error_code='', error_message='', updated_at=now() WHERE id=$1`,
		id, parseTitle, nullableInt(year), nullableInt(season), nullableInt(episode), source,
		technical.Resolution, technical.VideoCodec, technical.AudioCodec, technical.AudioChannels, technical.HDRFormat,
		mediaType, status, targetPath, movedPath, movedPath, finalName)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 不存在")
}

func (s *Store) RecountBatch(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `UPDATE batches b SET
		total=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id), 0),
		done=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$2), 0),
		failed=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$3), 0),
		incomplete_collection=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$4), 0)
		WHERE b.id=$1`,
		id, models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection)
	return err
}

func (s *Store) RefreshCollectionLocalCounts(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `WITH counts AS (
			SELECT c.tmdb_id,
				(COUNT(DISTINCT mm.movie_tmdb_id) FILTER (
					WHERE cm.released=true AND mf.process_status IN ($1,$2)
				))::INT AS local_count
			FROM collections c
			LEFT JOIN collection_movies cm ON cm.collection_id=c.tmdb_id
			LEFT JOIN media_matches mm ON mm.movie_tmdb_id=cm.movie_tmdb_id
			LEFT JOIN media_files mf ON mf.id=mm.file_id
			GROUP BY c.tmdb_id
		)
		UPDATE collections c SET local_count=counts.local_count,
			status=CASE WHEN c.movie_count > 0 AND counts.local_count >= c.movie_count THEN 'complete' ELSE 'incomplete' END,
			updated_at=now()
		FROM counts WHERE counts.tmdb_id=c.tmdb_id`,
		models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return err
	}
	return s.RefreshCuratedCollectionLocalCounts(ctx)
}

func (s *Store) ListMediaFiles(ctx context.Context, status, search string, limit, offset int) (models.MediaFilePage, error) {
	query := `SELECT id, batch_id, original_path, current_path, final_path, original_file_name, final_file_name,
		extension, file_size, file_hash, hash_type, media_type, process_status, match_status, parse_title,
		COALESCE(parse_year,0), COALESCE(season,0), COALESCE(episode,0), resolution, source, video_codec, audio_codec, audio_channels, hdr_format,
		planned_target, move_attempts, last_verified_path, error_code, error_message, created_at, updated_at FROM media_files`
	args := []any{}
	conditions := []string{}
	statuses := splitCSV(status)
	if len(statuses) > 0 {
		args = append(args, statuses)
		conditions = append(conditions, fmt.Sprintf("process_status=ANY($%d)", len(args)))
	}
	if search != "" {
		args = append(args, "%"+search+"%")
		p := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions, fmt.Sprintf(`(id ILIKE %[1]s OR batch_id ILIKE %[1]s OR original_path ILIKE %[1]s OR current_path ILIKE %[1]s
			OR final_path ILIKE %[1]s OR original_file_name ILIKE %[1]s OR final_file_name ILIKE %[1]s OR extension ILIKE %[1]s
			OR file_hash ILIKE %[1]s OR hash_type ILIKE %[1]s OR media_type ILIKE %[1]s OR process_status ILIKE %[1]s OR match_status ILIKE %[1]s
			OR parse_title ILIKE %[1]s OR COALESCE(parse_year,0)::TEXT ILIKE %[1]s OR COALESCE(season,0)::TEXT ILIKE %[1]s
			OR COALESCE(episode,0)::TEXT ILIKE %[1]s OR resolution ILIKE %[1]s OR source ILIKE %[1]s OR video_codec ILIKE %[1]s
			OR audio_codec ILIKE %[1]s OR audio_channels ILIKE %[1]s OR hdr_format ILIKE %[1]s OR planned_target ILIKE %[1]s
			OR error_code ILIKE %[1]s OR error_message ILIKE %[1]s)`, p))
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	var total int
	countQuery := `SELECT COUNT(*)::INT FROM media_files`
	if len(conditions) > 0 {
		countQuery += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return models.MediaFilePage{}, err
	}
	query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.Query(ctx, query, pageArgs...)
	if err != nil {
		return models.MediaFilePage{}, err
	}
	defer rows.Close()
	files := make([]models.MediaFile, 0)
	for rows.Next() {
		file, err := scanMediaFile(rows)
		if err != nil {
			return models.MediaFilePage{}, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return models.MediaFilePage{}, err
	}
	return models.MediaFilePage{Items: files, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) MediaStats(ctx context.Context) (models.MediaStats, error) {
	var stats models.MediaStats
	err := s.db.QueryRow(ctx, `WITH media AS (
			SELECT
				COUNT(*)::INT AS total,
				(COUNT(*) FILTER (WHERE process_status=$1))::INT AS done,
				(COUNT(*) FILTER (WHERE process_status=$2))::INT AS failed,
				(COUNT(*) FILTER (WHERE process_status=$3))::INT AS incomplete_collection
			FROM media_files
		),
		active_shows AS (
			SELECT DISTINCT mm.show_tmdb_id
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.show_tmdb_id > 0 AND mf.process_status IN ($1,$3)
		),
		episode_status AS (
			SELECT te.show_tmdb_id, te.season,
				EXISTS (
					SELECT 1
					FROM media_matches mm
					JOIN media_files mf ON mf.id=mm.file_id
					WHERE mm.episode_id=te.id AND mf.process_status IN ($1,$3)
				) AS local
			FROM tv_episodes te
			JOIN active_shows active ON active.show_tmdb_id=te.show_tmdb_id
			WHERE te.released=true
		),
		season_status AS (
			SELECT show_tmdb_id, season,
				(COUNT(*) FILTER (WHERE NOT local))::INT AS missing_count
			FROM episode_status
			GROUP BY show_tmdb_id, season
		),
		tv AS (
			SELECT
				COALESCE((SELECT COUNT(*)::INT FROM season_status WHERE missing_count > 0), 0) AS missing_season_count,
				COALESCE((SELECT COUNT(*)::INT FROM episode_status WHERE NOT local), 0) AS missing_episode_count
		)
		SELECT media.total, media.done, media.failed, media.incomplete_collection, tv.missing_season_count, tv.missing_episode_count
		FROM media, tv`,
		models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection).
		Scan(&stats.Total, &stats.Done, &stats.Failed, &stats.IncompleteCollection, &stats.MissingTVSeasonCount, &stats.MissingTVEpisodeCount)
	return stats, err
}

func (s *Store) UpsertMovie(ctx context.Context, movie models.MovieMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO movies (tmdb_id, imdb_id, title, original_title, year, release_date, overview, runtime, genres, genre_ids, original_language, production_countries, keywords, rating, poster_path, backdrop_path, collection_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tmdb_id) DO UPDATE SET imdb_id=$2, title=$3, original_title=$4, year=$5, release_date=$6, overview=$7,
		runtime=$8, genres=$9, genre_ids=$10, original_language=$11, production_countries=$12, keywords=$13, rating=$14, poster_path=$15, backdrop_path=$16, collection_id=$17, updated_at=now()`,
		movie.TMDBID, movie.IMDBID, movie.Title, movie.Original, movie.Year, movie.ReleaseDate, movie.Overview, movie.Runtime,
		movie.Genres, movie.GenreIDs, movie.OriginalLanguage, movie.ProductionCountries, movie.Keywords, movie.Rating, movie.PosterPath, movie.BackdropPath, movie.CollectionID)
	return err
}

func (s *Store) UpsertTVShow(ctx context.Context, show models.TVShowMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO tv_shows (tmdb_id, tvdb_id, name, original_name, year, first_air_date, overview, season_count, episode_count, genres, genre_ids, original_language, origin_country, keywords, poster_path, backdrop_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tmdb_id) DO UPDATE SET tvdb_id=$2, name=$3, original_name=$4, year=$5, first_air_date=$6,
		overview=$7, season_count=$8, episode_count=$9, genres=$10, genre_ids=$11, original_language=$12, origin_country=$13, keywords=$14, poster_path=$15, backdrop_path=$16, updated_at=now()`,
		show.TMDBID, show.TVDBID, show.Name, show.Original, show.Year, show.FirstAirDate, show.Overview, show.SeasonCount, show.EpisodeCount,
		show.Genres, show.GenreIDs, show.OriginalLanguage, show.OriginCountry, show.Keywords, show.PosterPath, show.BackdropPath)
	return err
}

func (s *Store) UpsertTVEpisode(ctx context.Context, episode models.TVEpisodeMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO tv_episodes (id, show_tmdb_id, tmdb_id, season, episode, title, air_date, released, overview, runtime, still_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (show_tmdb_id, season, episode) DO UPDATE SET tmdb_id=$3, title=$6, air_date=$7, released=$8, overview=$9, runtime=$10, still_path=$11, updated_at=now()`,
		episode.ID, episode.ShowTMDBID, episode.TMDBID, episode.Season, episode.Episode, episode.Title, episode.AirDate, episode.Released, episode.Overview, episode.Runtime, episode.StillPath)
	return err
}

func (s *Store) DeleteTVEpisodesNotIn(ctx context.Context, showID int, episodeIDs []string) error {
	if len(episodeIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `DELETE FROM tv_episodes WHERE show_tmdb_id=$1 AND NOT (id = ANY($2))`, showID, episodeIDs)
	return err
}

func (s *Store) TVShows(ctx context.Context, search string, limit, offset int) (models.TVShowPage, error) {
	args := []any{models.StatusDone, models.StatusIncompleteCollection}
	where := `WHERE EXISTS (
		SELECT 1
		FROM media_matches mm
		JOIN media_files mf ON mf.id=mm.file_id
		WHERE mm.show_tmdb_id=s.tmdb_id AND mf.process_status IN ($1,$2)
	)`
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+search+"%")
		where += fmt.Sprintf(` AND (s.name ILIKE %[1]s OR s.original_name ILIKE %[1]s OR s.tmdb_id::TEXT ILIKE %[1]s OR s.overview ILIKE %[1]s)`, fmt.Sprintf("$%d", len(args)))
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::INT FROM tv_shows s `+where, args...).Scan(&total); err != nil {
		return models.TVShowPage{}, err
	}
	args = append(args, limit, offset)
	order := fmt.Sprintf(`ORDER BY s.updated_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := s.db.Query(ctx, tvShowStatusSQL(where, order), args...)
	if err != nil {
		return models.TVShowPage{}, err
	}
	defer rows.Close()
	items, err := scanTVShowStatuses(rows)
	if err != nil {
		return models.TVShowPage{}, err
	}
	return models.TVShowPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) TVShow(ctx context.Context, id int) (models.TVShowStatus, error) {
	rows, err := s.db.Query(ctx, tvShowStatusSQL(`WHERE s.tmdb_id=$3`, ``), models.StatusDone, models.StatusIncompleteCollection, id)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	defer rows.Close()
	shows, err := scanTVShowStatuses(rows)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	if len(shows) == 0 {
		return models.TVShowStatus{}, pgx.ErrNoRows
	}
	seasons, err := s.TVSeasons(ctx, id)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	shows[0].Seasons = seasons
	return shows[0], nil
}

func (s *Store) TVSeasons(ctx context.Context, showID int) ([]models.TVSeasonStatus, error) {
	rows, err := s.db.Query(ctx, `SELECT te.id, te.show_tmdb_id, te.tmdb_id, te.season, te.episode, te.title, te.air_date, te.released,
		te.overview, te.runtime, te.still_path, COALESCE(local.file_id, ''), COALESCE(local.file_path, ''), COALESCE(local.process_status, '')
		FROM tv_episodes te
		LEFT JOIN LATERAL (
			SELECT mf.id AS file_id, COALESCE(NULLIF(mf.final_path, ''), mf.current_path) AS file_path, mf.process_status
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.episode_id=te.id AND mf.process_status IN ($2,$3)
			ORDER BY mf.updated_at DESC
			LIMIT 1
		) local ON true
		WHERE te.show_tmdb_id=$1
		ORDER BY te.season, te.episode`, showID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seasonOrder := make([]int, 0)
	bySeason := map[int]*models.TVSeasonStatus{}
	for rows.Next() {
		var episode models.TVEpisodeMetadata
		if err := rows.Scan(&episode.ID, &episode.ShowTMDBID, &episode.TMDBID, &episode.Season, &episode.Episode, &episode.Title, &episode.AirDate, &episode.Released, &episode.Overview, &episode.Runtime, &episode.StillPath, &episode.FileID, &episode.FilePath, &episode.ProcessStatus); err != nil {
			return nil, err
		}
		episode.Local = episode.FileID != ""
		season, ok := bySeason[episode.Season]
		if !ok {
			season = &models.TVSeasonStatus{Season: episode.Season, Episodes: make([]models.TVEpisodeMetadata, 0)}
			bySeason[episode.Season] = season
			seasonOrder = append(seasonOrder, episode.Season)
		}
		season.EpisodeCount++
		if episode.Released {
			season.ReleasedEpisodeCount++
			if episode.Local {
				season.LocalEpisodeCount++
			} else {
				season.MissingEpisodeCount++
			}
		} else {
			season.UnreleasedEpisodeCount++
		}
		season.Episodes = append(season.Episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seasons := make([]models.TVSeasonStatus, 0, len(seasonOrder))
	for _, number := range seasonOrder {
		season := bySeason[number]
		season.Status = tvAvailabilityStatus(season.ReleasedEpisodeCount, season.MissingEpisodeCount)
		seasons = append(seasons, *season)
	}
	return seasons, nil
}

func tvShowStatusSQL(where, order string) string {
	return `WITH episode_status AS (
		SELECT te.show_tmdb_id, te.season, te.released,
			EXISTS (
				SELECT 1
				FROM media_matches mm
				JOIN media_files mf ON mf.id=mm.file_id
				WHERE mm.episode_id=te.id AND mf.process_status IN ($1,$2)
			) AS local
		FROM tv_episodes te
	),
	show_stats AS (
		SELECT show_tmdb_id,
			(COUNT(*) FILTER (WHERE released))::INT AS released_count,
			(COUNT(*) FILTER (WHERE NOT released))::INT AS unreleased_count,
			(COUNT(*) FILTER (WHERE released AND local))::INT AS local_count,
			(COUNT(*) FILTER (WHERE released AND NOT local))::INT AS missing_count
		FROM episode_status
		GROUP BY show_tmdb_id
	),
	season_stats AS (
		SELECT show_tmdb_id, season,
			(COUNT(*) FILTER (WHERE released AND NOT local))::INT AS missing_count
		FROM episode_status
		GROUP BY show_tmdb_id, season
	),
	missing_seasons AS (
		SELECT show_tmdb_id, (COUNT(*) FILTER (WHERE missing_count > 0))::INT AS missing_season_count
		FROM season_stats
		GROUP BY show_tmdb_id
	)
	SELECT s.tmdb_id, s.name, s.original_name, s.year, s.first_air_date, s.overview, s.season_count, s.episode_count,
		COALESCE(ss.released_count, 0), COALESCE(ss.unreleased_count, 0), COALESCE(ss.local_count, 0), COALESCE(ss.missing_count, 0),
		COALESCE(ms.missing_season_count, 0),
		CASE
			WHEN COALESCE(ss.released_count, 0)=0 THEN 'unknown'
			WHEN COALESCE(ss.missing_count, 0)=0 THEN 'complete'
			ELSE 'incomplete'
		END,
		s.poster_path, s.backdrop_path
	FROM tv_shows s
	LEFT JOIN show_stats ss ON ss.show_tmdb_id=s.tmdb_id
	LEFT JOIN missing_seasons ms ON ms.show_tmdb_id=s.tmdb_id ` + where + ` ` + order
}

func scanTVShowStatuses(rows pgx.Rows) ([]models.TVShowStatus, error) {
	shows := make([]models.TVShowStatus, 0)
	for rows.Next() {
		var show models.TVShowStatus
		if err := rows.Scan(&show.TMDBID, &show.Name, &show.Original, &show.Year, &show.FirstAirDate, &show.Overview, &show.SeasonCount, &show.EpisodeCount, &show.ReleasedEpisodeCount, &show.UnreleasedEpisodeCount, &show.LocalEpisodeCount, &show.MissingEpisodeCount, &show.MissingSeasonCount, &show.Status, &show.PosterPath, &show.BackdropPath); err != nil {
			return nil, err
		}
		shows = append(shows, show)
	}
	return shows, rows.Err()
}

func tvAvailabilityStatus(releasedCount, missingCount int) string {
	if releasedCount == 0 {
		return "unknown"
	}
	if missingCount == 0 {
		return "complete"
	}
	return "incomplete"
}

func (s *Store) UpsertCollection(ctx context.Context, collection models.CollectionMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collections (tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tmdb_id) DO UPDATE SET name=$2, overview=$3, movie_count=$4, unreleased_count=$5, poster_path=$8, backdrop_path=$9, updated_at=now()`,
		collection.TMDBID, collection.Name, collection.Overview, collection.MovieCount, collection.UnreleasedCount, collection.LocalCount, collection.Status, collection.PosterPath, collection.BackdropPath)
	return err
}

func (s *Store) UpsertCollectionMovie(ctx context.Context, movie models.CollectionMovieMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collection_movies (collection_id, movie_tmdb_id, title, release_date, released, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (collection_id, movie_tmdb_id) DO UPDATE SET title=$3, release_date=$4, released=$5, sort_order=$6`,
		movie.CollectionID, movie.MovieTMDBID, movie.Title, movie.ReleaseDate, movie.Released, movie.SortOrder)
	return err
}

func (s *Store) UpsertMediaMatch(ctx context.Context, fileID, targetType string, movieID, showID int, episodeID string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO media_matches (file_id, target_type, movie_tmdb_id, show_tmdb_id, episode_id, confidence)
		VALUES ($1,$2,$3,$4,$5,1)
		ON CONFLICT (file_id) DO UPDATE SET target_type=$2, movie_tmdb_id=$3, show_tmdb_id=$4, episode_id=$5`,
		fileID, targetType, movieID, showID, episodeID)
	return err
}

func (s *Store) CollectionPartIDs(ctx context.Context, collectionID int) ([]int, error) {
	rows, err := s.db.Query(ctx, `SELECT movie_tmdb_id FROM collection_movies WHERE collection_id=$1 AND released=true`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInts(rows)
}

func (s *Store) LocalCollectionMovieIDs(ctx context.Context, collectionID int) ([]int, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT mm.movie_tmdb_id
		FROM media_matches mm
		JOIN media_files mf ON mf.id=mm.file_id
		JOIN collection_movies cm ON cm.movie_tmdb_id=mm.movie_tmdb_id
		WHERE cm.collection_id=$1 AND cm.released=true AND mf.process_status IN ($2,$3)`,
		collectionID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInts(rows)
}

func (s *Store) UpdateCollectionStatus(ctx context.Context, collectionID, localCount int, status string) error {
	_, err := s.db.Exec(ctx, `UPDATE collections SET local_count=$2, status=$3, updated_at=now() WHERE tmdb_id=$1`, collectionID, localCount, status)
	return err
}

func (s *Store) RefreshCuratedCollectionLocalCounts(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `WITH counts AS (
			SELECT cc.id,
				(COUNT(DISTINCT ccm.douban_id))::INT AS item_count,
				(COUNT(DISTINCT ccm.douban_id) FILTER (WHERE ccm.movie_tmdb_id > 0))::INT AS resolved_count,
				(COUNT(DISTINCT ccm.douban_id) FILTER (WHERE ccm.movie_tmdb_id = 0))::INT AS unresolved_count,
				(COUNT(DISTINCT ccm.movie_tmdb_id) FILTER (
					WHERE ccm.movie_tmdb_id > 0 AND mf.process_status IN ($1,$2)
				))::INT AS local_count
			FROM curated_collections cc
			LEFT JOIN curated_collection_movies ccm ON ccm.list_id=cc.id
			LEFT JOIN media_matches mm ON mm.movie_tmdb_id=ccm.movie_tmdb_id
			LEFT JOIN media_files mf ON mf.id=mm.file_id
			GROUP BY cc.id
		)
		UPDATE curated_collections cc SET
			item_count=counts.item_count,
			resolved_count=counts.resolved_count,
			unresolved_count=counts.unresolved_count,
			local_count=counts.local_count,
			missing_count=GREATEST(counts.resolved_count-counts.local_count, 0),
			status=CASE
				WHEN counts.resolved_count > 0 AND counts.local_count >= counts.resolved_count AND counts.unresolved_count = 0 THEN 'complete'
				ELSE 'incomplete'
			END,
			updated_at=now()
		FROM counts WHERE counts.id=cc.id`, models.StatusDone, models.StatusIncompleteCollection)
	return err
}

func (s *Store) CuratedCollectionMovieMap(ctx context.Context, listID string) (map[string]models.CollectionMovieMetadata, error) {
	rows, err := s.db.Query(ctx, `SELECT list_id, rank, douban_id, imdb_id, movie_tmdb_id, title, original_title, year,
		release_date, rating, poster_path, backdrop_path, source_url, match_status, error_message
		FROM curated_collection_movies WHERE list_id=$1`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]models.CollectionMovieMetadata{}
	for rows.Next() {
		var item models.CollectionMovieMetadata
		if err := rows.Scan(&item.ListID, &item.SortOrder, &item.DoubanID, &item.IMDBID, &item.MovieTMDBID, &item.Title, &item.OriginalTitle,
			&item.Year, &item.ReleaseDate, &item.Rating, &item.PosterPath, &item.BackdropPath, &item.SourceURL, &item.MatchStatus, &item.ErrorMessage); err != nil {
			return nil, err
		}
		item.Released = true
		item.Resolved = item.MovieTMDBID > 0
		items[item.DoubanID] = item
	}
	return items, rows.Err()
}

func (s *Store) ReplaceCuratedCollectionMovies(ctx context.Context, listID string, items []models.CollectionMovieMetadata) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	doubanIDs := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.DoubanID) != "" {
			doubanIDs = append(doubanIDs, item.DoubanID)
		}
	}
	if len(doubanIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM curated_collection_movies WHERE list_id=$1 AND NOT (douban_id = ANY($2))`, listID, doubanIDs); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM curated_collection_movies WHERE list_id=$1`, listID); err != nil {
		return err
	}
	for _, item := range items {
		item.ListID = listID
		if item.SortOrder <= 0 {
			item.SortOrder = len(doubanIDs)
		}
		matchStatus := strings.TrimSpace(item.MatchStatus)
		if matchStatus == "" {
			if item.MovieTMDBID > 0 {
				matchStatus = "matched"
			} else {
				matchStatus = "unresolved"
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO curated_collection_movies
			(list_id, rank, douban_id, imdb_id, movie_tmdb_id, title, original_title, year, release_date, rating,
				poster_path, backdrop_path, source_url, match_status, error_message)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (list_id, douban_id) DO UPDATE SET rank=$2, imdb_id=$4, movie_tmdb_id=$5, title=$6,
				original_title=$7, year=$8, release_date=$9, rating=$10, poster_path=$11, backdrop_path=$12,
				source_url=$13, match_status=$14, error_message=$15, updated_at=now()`,
			listID, item.SortOrder, item.DoubanID, item.IMDBID, item.MovieTMDBID, item.Title, item.OriginalTitle,
			item.Year, item.ReleaseDate, item.Rating, item.PosterPath, item.BackdropPath, item.SourceURL, matchStatus, item.ErrorMessage); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE curated_collections SET item_count=$2, refreshed_at=now(), last_refresh_error='', updated_at=now() WHERE id=$1`, listID, len(items)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.RefreshCuratedCollectionLocalCounts(ctx)
}

func (s *Store) MarkCuratedCollectionRefreshError(ctx context.Context, listID, message string) error {
	_, err := s.db.Exec(ctx, `UPDATE curated_collections SET last_refresh_error=$2, updated_at=now() WHERE id=$1`, listID, message)
	return err
}

func (s *Store) CompleteCollectionRepairCandidates(ctx context.Context) ([]CollectionRepairCandidate, error) {
	rows, err := s.db.Query(ctx, `WITH stats AS (
			SELECT c.tmdb_id, c.name, c.overview, c.movie_count, c.unreleased_count, c.poster_path, c.backdrop_path,
				(COUNT(DISTINCT mm.movie_tmdb_id) FILTER (
					WHERE cm.released=true AND mf.process_status IN ($1,$2)
				))::INT AS local_count,
				COALESCE(BOOL_OR(mf.process_status=$2), false) AS has_incomplete
			FROM collections c
			JOIN collection_movies cm ON cm.collection_id=c.tmdb_id
			LEFT JOIN media_matches mm ON mm.movie_tmdb_id=cm.movie_tmdb_id
			LEFT JOIN media_files mf ON mf.id=mm.file_id
			GROUP BY c.tmdb_id
		)
		SELECT tmdb_id, name, overview, movie_count, unreleased_count, local_count, poster_path, backdrop_path
		FROM stats
		WHERE movie_count > 0 AND local_count >= movie_count AND has_incomplete=true`,
		models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]CollectionRepairCandidate, 0)
	for rows.Next() {
		var candidate CollectionRepairCandidate
		candidate.Collection.Status = "complete"
		if err := rows.Scan(&candidate.Collection.TMDBID, &candidate.Collection.Name, &candidate.Collection.Overview, &candidate.Collection.MovieCount,
			&candidate.Collection.UnreleasedCount, &candidate.Collection.LocalCount, &candidate.Collection.PosterPath, &candidate.Collection.BackdropPath); err != nil {
			return nil, err
		}
		paths, err := s.IncompleteCollectionFilePaths(ctx, candidate.Collection.TMDBID)
		if err != nil {
			return nil, err
		}
		candidate.FilePaths = paths
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (s *Store) IncompleteCollectionFilePaths(ctx context.Context, collectionID int) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT COALESCE(NULLIF(mf.final_path, ''), mf.current_path)
		FROM media_matches mm
		JOIN media_files mf ON mf.id=mm.file_id
		JOIN collection_movies cm ON cm.movie_tmdb_id=mm.movie_tmdb_id
		WHERE cm.collection_id=$1 AND mf.process_status=$2
			AND COALESCE(NULLIF(mf.final_path, ''), mf.current_path) <> ''
		ORDER BY 1`,
		collectionID, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		paths = append(paths, value)
	}
	return paths, rows.Err()
}

func (s *Store) Collections(ctx context.Context, search, status string, limit, offset int) (models.CollectionPage, error) {
	curated, err := s.CuratedCollections(ctx, search, status)
	if err != nil {
		return models.CollectionPage{}, err
	}
	tmdbTotal, err := s.tmdbCollectionCount(ctx, search, status)
	if err != nil {
		return models.CollectionPage{}, err
	}
	total := tmdbTotal + len(curated)
	items := make([]models.CollectionMetadata, 0, limit)
	remaining := limit
	tmdbOffset := 0
	if offset < len(curated) {
		end := offset + remaining
		if end > len(curated) {
			end = len(curated)
		}
		items = append(items, curated[offset:end]...)
		remaining -= end - offset
	} else {
		tmdbOffset = offset - len(curated)
	}
	if remaining > 0 {
		tmdbItems, err := s.tmdbCollections(ctx, search, status, remaining, tmdbOffset)
		if err != nil {
			return models.CollectionPage{}, err
		}
		items = append(items, tmdbItems...)
	}
	return models.CollectionPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) tmdbCollectionConditions(search, status string) ([]any, []string) {
	args := []any{}
	conditions := []string{}
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+search+"%")
		conditions = append(conditions, fmt.Sprintf(`(name ILIKE %[1]s OR tmdb_id::TEXT ILIKE %[1]s OR overview ILIKE %[1]s OR status ILIKE %[1]s)`, fmt.Sprintf("$%d", len(args))))
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete":
		conditions = append(conditions, `status='complete'`)
	case "incomplete":
		conditions = append(conditions, `status<>'complete'`)
	}
	return args, conditions
}

func (s *Store) tmdbCollectionCount(ctx context.Context, search, status string) (int, error) {
	args, conditions := s.tmdbCollectionConditions(search, status)
	where := ""
	if len(conditions) > 0 {
		where = ` WHERE ` + strings.Join(conditions, " AND ")
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::INT FROM collections`+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) tmdbCollections(ctx context.Context, search, status string, limit, offset int) ([]models.CollectionMetadata, error) {
	args, conditions := s.tmdbCollectionConditions(search, status)
	where := ""
	if len(conditions) > 0 {
		where = ` WHERE ` + strings.Join(conditions, " AND ")
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path
		FROM collections%s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collections := make([]models.CollectionMetadata, 0)
	for rows.Next() {
		var c models.CollectionMetadata
		if err := rows.Scan(&c.TMDBID, &c.Name, &c.Overview, &c.MovieCount, &c.UnreleasedCount, &c.LocalCount, &c.Status, &c.PosterPath, &c.BackdropPath); err != nil {
			return nil, err
		}
		c.ID = fmt.Sprintf("%d", c.TMDBID)
		c.Kind = models.CollectionKindTMDB
		collections = append(collections, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collections, nil
}

func (s *Store) CuratedCollections(ctx context.Context, search, status string) ([]models.CollectionMetadata, error) {
	args := []any{}
	conditions := []string{}
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+search+"%")
		conditions = append(conditions, fmt.Sprintf(`(id ILIKE %[1]s OR source ILIKE %[1]s OR name ILIKE %[1]s OR overview ILIKE %[1]s OR status ILIKE %[1]s)`, fmt.Sprintf("$%d", len(args))))
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete":
		conditions = append(conditions, `status='complete'`)
	case "incomplete":
		conditions = append(conditions, `status<>'complete'`)
	}
	where := ""
	if len(conditions) > 0 {
		where = ` WHERE ` + strings.Join(conditions, " AND ")
	}
	rows, err := s.db.Query(ctx, `SELECT id, source, name, overview, source_url, item_count, unresolved_count,
		local_count, status, poster_path, backdrop_path, refreshed_at, last_refresh_error
		FROM curated_collections`+where+`
		ORDER BY CASE WHEN id=$`+fmt.Sprint(len(args)+1)+` THEN 0 ELSE 1 END, updated_at DESC`, append(args, models.CuratedDoubanTop250ID)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]models.CollectionMetadata, 0)
	for rows.Next() {
		item, err := scanCuratedCollection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Collection(ctx context.Context, id int) (models.CollectionMetadata, error) {
	var c models.CollectionMetadata
	err := s.db.QueryRow(ctx, `SELECT tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path FROM collections WHERE tmdb_id=$1`, id).
		Scan(&c.TMDBID, &c.Name, &c.Overview, &c.MovieCount, &c.UnreleasedCount, &c.LocalCount, &c.Status, &c.PosterPath, &c.BackdropPath)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	c.ID = fmt.Sprintf("%d", c.TMDBID)
	c.Kind = models.CollectionKindTMDB
	parts, err := s.CollectionMovies(ctx, id)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	c.Parts = parts
	return c, nil
}

func (s *Store) CuratedCollection(ctx context.Context, id string) (models.CollectionMetadata, error) {
	row := s.db.QueryRow(ctx, `SELECT id, source, name, overview, source_url, item_count, unresolved_count,
		local_count, status, poster_path, backdrop_path, refreshed_at, last_refresh_error
		FROM curated_collections WHERE id=$1`, id)
	collection, err := scanCuratedCollection(row)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	parts, err := s.CuratedCollectionMovies(ctx, id)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	collection.Parts = parts
	return collection, nil
}

func (s *Store) CollectionMovies(ctx context.Context, collectionID int) ([]models.CollectionMovieMetadata, error) {
	rows, err := s.db.Query(ctx, `SELECT cm.collection_id, cm.movie_tmdb_id, cm.title, cm.release_date, cm.released, cm.sort_order,
		COALESCE(local.file_id, ''), COALESCE(local.file_path, ''), COALESCE(local.process_status, '')
		FROM collection_movies cm
		LEFT JOIN LATERAL (
			SELECT mf.id AS file_id, COALESCE(NULLIF(mf.final_path, ''), mf.current_path) AS file_path, mf.process_status
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.movie_tmdb_id=cm.movie_tmdb_id AND mf.process_status IN ($2,$3)
			ORDER BY mf.updated_at DESC
			LIMIT 1
		) local ON true
		WHERE cm.collection_id=$1
		ORDER BY cm.sort_order, cm.release_date, cm.movie_tmdb_id`,
		collectionID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := make([]models.CollectionMovieMetadata, 0)
	for rows.Next() {
		var item models.CollectionMovieMetadata
		if err := rows.Scan(&item.CollectionID, &item.MovieTMDBID, &item.Title, &item.ReleaseDate, &item.Released, &item.SortOrder, &item.FileID, &item.FilePath, &item.ProcessStatus); err != nil {
			return nil, err
		}
		item.Local = item.FileID != ""
		item.Resolved = item.MovieTMDBID > 0
		parts = append(parts, item)
	}
	return parts, rows.Err()
}

func (s *Store) CuratedCollectionMovies(ctx context.Context, listID string) ([]models.CollectionMovieMetadata, error) {
	rows, err := s.db.Query(ctx, `SELECT ccm.list_id, ccm.rank, ccm.douban_id, ccm.imdb_id, ccm.movie_tmdb_id,
		ccm.title, ccm.original_title, ccm.year, ccm.release_date, ccm.rating, ccm.poster_path, ccm.backdrop_path,
		ccm.source_url, ccm.match_status, ccm.error_message,
		COALESCE(local.file_id, ''), COALESCE(local.file_path, ''), COALESCE(local.process_status, '')
		FROM curated_collection_movies ccm
		LEFT JOIN LATERAL (
			SELECT mf.id AS file_id, COALESCE(NULLIF(mf.final_path, ''), mf.current_path) AS file_path, mf.process_status
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE ccm.movie_tmdb_id > 0 AND mm.movie_tmdb_id=ccm.movie_tmdb_id AND mf.process_status IN ($2,$3)
			ORDER BY mf.updated_at DESC
			LIMIT 1
		) local ON true
		WHERE ccm.list_id=$1
		ORDER BY ccm.rank, ccm.year, ccm.title`, listID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := make([]models.CollectionMovieMetadata, 0)
	for rows.Next() {
		var item models.CollectionMovieMetadata
		if err := rows.Scan(&item.ListID, &item.SortOrder, &item.DoubanID, &item.IMDBID, &item.MovieTMDBID,
			&item.Title, &item.OriginalTitle, &item.Year, &item.ReleaseDate, &item.Rating, &item.PosterPath, &item.BackdropPath,
			&item.SourceURL, &item.MatchStatus, &item.ErrorMessage, &item.FileID, &item.FilePath, &item.ProcessStatus); err != nil {
			return nil, err
		}
		item.Released = true
		item.Resolved = item.MovieTMDBID > 0
		item.Local = item.FileID != ""
		parts = append(parts, item)
	}
	return parts, rows.Err()
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanCuratedCollection(row scanRow) (models.CollectionMetadata, error) {
	var c models.CollectionMetadata
	var refreshedAt sql.NullTime
	err := row.Scan(&c.ID, &c.Source, &c.Name, &c.Overview, &c.SourceURL, &c.MovieCount, &c.UnresolvedCount,
		&c.LocalCount, &c.Status, &c.PosterPath, &c.BackdropPath, &refreshedAt, &c.LastRefreshError)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	c.Kind = models.CollectionKindCurated
	c.UnreleasedCount = c.UnresolvedCount
	if refreshedAt.Valid {
		c.LastRefreshedAt = &refreshedAt.Time
	}
	return c, nil
}

func (s *Store) CreateTask(ctx context.Context, id, fileID, batchID, templateID, sourcePath, targetPath, status string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO organize_tasks (id, file_id, batch_id, template_id, source_path, target_path, task_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, fileID, batchID, templateID, sourcePath, targetPath, status)
	return err
}

func (s *Store) FinishTask(ctx context.Context, id, status, code, message string) error {
	_, err := s.db.Exec(ctx, `UPDATE organize_tasks SET task_status=$2, error_code=$3, error_message=$4, executed_at=now() WHERE id=$1`, id, status, code, message)
	return err
}

func (s *Store) AddHistory(ctx context.Context, id, taskID, fileID, batchID, sourcePath, targetPath, name, status, code, message string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO operation_histories (id, task_id, file_id, batch_id, source_path, target_path, operation_name, operation_status, error_code, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, taskID, fileID, batchID, sourcePath, targetPath, name, status, code, message)
	return err
}

func (s *Store) AddCollectionCompletionHistory(ctx context.Context, id string, collectionID int, name, sourcePath, targetPath string, movieCount int) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collection_completion_histories (id, collection_id, collection_tmdb_id, collection_name, source_path, target_path, movie_count)
		VALUES ($1,$2,$2,$3,$4,$5,$6)`, id, collectionID, name, sourcePath, targetPath, movieCount)
	return err
}

func (s *Store) UpdateCollectionPathPrefix(ctx context.Context, collectionID int, sourcePrefix, targetPrefix string) ([]string, error) {
	rows, err := s.db.Query(ctx, `WITH updated AS (
			UPDATE media_files mf SET
				current_path=replace(current_path, $2, $3),
				final_path=replace(final_path, $2, $3),
				last_verified_path=replace(last_verified_path, $2, $3),
				process_status=$4,
				updated_at=now()
			FROM media_matches mm
			JOIN collection_movies cm ON cm.movie_tmdb_id=mm.movie_tmdb_id
			WHERE mf.id=mm.file_id AND cm.collection_id=$1 AND mf.process_status=$5
			RETURNING mf.batch_id
		)
		SELECT DISTINCT batch_id FROM updated`,
		collectionID, sourcePrefix, targetPrefix, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batchIDs := make([]string, 0)
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			return nil, err
		}
		if batchID != "" {
			batchIDs = append(batchIDs, batchID)
		}
	}
	return batchIDs, rows.Err()
}

func scanBatch(rows pgx.Rows) (models.Batch, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := rows.Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, err
}

func appendLogRows(rows pgx.Rows, entries *[]models.LogEntry) error {
	defer rows.Close()
	for rows.Next() {
		var entry models.LogEntry
		if err := rows.Scan(&entry.ID, &entry.Type, &entry.Source, &entry.Status, &entry.Message, &entry.Detail,
			&entry.BatchID, &entry.FileID, &entry.FileName, &entry.Path, &entry.Model, &entry.BaseURL, &entry.ProxyURL,
			&entry.ResponseFormat, &entry.RequestJSON, &entry.ResponseJSON, &entry.ParsedJSON, &entry.HTTPStatus,
			&entry.DurationMS, &entry.ErrorMessage, &entry.CreatedAt); err != nil {
			return err
		}
		*entries = append(*entries, entry)
	}
	return rows.Err()
}

func normalizeLogType(logType string) string {
	logType = strings.TrimSpace(logType)
	if logType == "" {
		return "all"
	}
	if logType == "scan" {
		return "scan_batch"
	}
	return logType
}

func logQueryFragments(logType string, includePayloads bool) []string {
	logType = normalizeLogType(logType)
	payloadJSON := `''`
	if includePayloads {
		payloadJSON = `request_json`
	}
	responseJSON := `''`
	if includePayloads {
		responseJSON = `response_json`
	}
	parsedJSON := `''`
	if includePayloads {
		parsedJSON = `parsed_json`
	}
	fragments := []string{}
	if logType == "all" || logType == "ai_filename" {
		fragments = append(fragments, `SELECT id, 'ai_filename' AS type, 'AI 文件名' AS source, status,
			CASE WHEN status='success' THEN COALESCE(NULLIF(title, ''), file_name, file_path) ELSE 'AI 文件名识别失败' END AS message,
			reason AS detail, batch_id, file_id, file_name, file_path AS path, model, base_url, proxy_url, response_format,
			`+payloadJSON+` AS request_json, `+responseJSON+` AS response_json, `+parsedJSON+` AS parsed_json,
			http_status, duration_ms, error_message, created_at
			FROM ai_filename_logs`)
	}
	if logType == "all" || logType == "p115_sync" {
		fragments = append(fragments, `SELECT id, 'p115_sync' AS type, '115 STRM' AS source, status,
			concat(trigger, ' / ', COALESCE(NULLIF(mode, ''), '-'), ' / 生成 ', generated, ' 更新 ', updated_count, ' 删除 ', deleted_count, ' 失败 ', failed) AS message,
			event_summary AS detail, '' AS batch_id, '' AS file_id, '' AS file_name, tree_version AS path, '' AS model,
			'' AS base_url, '' AS proxy_url, '' AS response_format, '' AS request_json, '' AS response_json, '' AS parsed_json,
			0 AS http_status, duration_ms, error_message, started_at AS created_at
			FROM p115_sync_runs`)
	}
	if logType == "all" || logType == "operation" {
		fragments = append(fragments, `SELECT id, 'operation' AS type, '整理操作' AS source, operation_status AS status, operation_name AS message,
			concat(source_path, ' -> ', target_path) AS detail, batch_id, file_id, '' AS file_name, source_path AS path, '' AS model,
			'' AS base_url, '' AS proxy_url, '' AS response_format, '' AS request_json, '' AS response_json, '' AS parsed_json,
			0 AS http_status, 0 AS duration_ms, error_message, created_at
			FROM operation_histories`)
		fragments = append(fragments, `SELECT id, 'organize_task' AS type, '整理任务' AS source, task_status AS status, template_id AS message,
			concat(source_path, ' -> ', target_path) AS detail, batch_id, file_id, '' AS file_name, source_path AS path, '' AS model,
			'' AS base_url, '' AS proxy_url, '' AS response_format, '' AS request_json, '' AS response_json, '' AS parsed_json,
			0 AS http_status, 0 AS duration_ms, error_message, COALESCE(executed_at, created_at) AS created_at
			FROM organize_tasks`)
		fragments = append(fragments, `SELECT id, 'collection_completion' AS type, '合集补齐' AS source, 'done' AS status, collection_name AS message,
			concat(source_path, ' -> ', target_path, ' / ', movie_count, ' 部') AS detail, '' AS batch_id, '' AS file_id,
			'' AS file_name, source_path AS path, '' AS model, '' AS base_url, '' AS proxy_url, '' AS response_format,
			'' AS request_json, '' AS response_json, '' AS parsed_json, 0 AS http_status, 0 AS duration_ms, '' AS error_message, created_at
			FROM collection_completion_histories`)
	}
	if logType == "all" || logType == "scan_batch" {
		fragments = append(fragments, `SELECT id, 'scan_batch' AS type, '扫描批次' AS source, status,
			concat(source, ' / 总数 ', total, ' 完成 ', done, ' 失败 ', failed, ' 缺失合集 ', incomplete_collection) AS message,
			'' AS detail, id AS batch_id, '' AS file_id, '' AS file_name, source AS path, '' AS model, '' AS base_url,
			'' AS proxy_url, '' AS response_format, '' AS request_json, '' AS response_json, '' AS parsed_json, 0 AS http_status,
			COALESCE((EXTRACT(EPOCH FROM (ended_at - started_at)) * 1000)::BIGINT, 0) AS duration_ms, '' AS error_message, started_at AS created_at
			FROM batches`)
	}
	return fragments
}

func scanMediaFile(rows pgx.Rows) (models.MediaFile, error) {
	var file models.MediaFile
	err := rows.Scan(&file.ID, &file.BatchID, &file.OriginalPath, &file.CurrentPath, &file.FinalPath, &file.OriginalName, &file.FinalName,
		&file.Extension, &file.FileSize, &file.FileHash, &file.HashType, &file.MediaType, &file.ProcessStatus, &file.MatchStatus, &file.ParseTitle,
		&file.ParseYear, &file.Season, &file.Episode, &file.Resolution, &file.Source, &file.VideoCodec, &file.AudioCodec, &file.AudioChannels, &file.HDRFormat, &file.PlannedTarget, &file.MoveAttempts,
		&file.LastVerified, &file.ErrorCode, &file.ErrorMessage, &file.CreatedAt, &file.UpdatedAt)
	return file, err
}

func scanSTRMLink(rows pgx.Rows) (models.STRMLink, error) {
	var link models.STRMLink
	var resolved sql.NullTime
	var mediaProbed sql.NullTime
	err := rows.Scan(&link.ID, &link.Provider, &link.LibraryCID, &link.LibraryName, &link.LibraryType, &link.RelativePath, &link.RemotePath,
		&link.RemoteFileID, &link.PickCode, &link.SHA1, &link.Size, &link.STRMPath, &link.PlayPath, &link.SourceTreeHash, &link.TreeVersion,
		&link.ResolveStatus, &link.Status, &link.ErrorCode, &link.ErrorMessage, &link.MediaStreams, &link.MediaDurationTicks, &mediaProbed, &link.MediaProbeError,
		&link.GeneratedAt, &resolved, &link.UpdatedAt)
	if resolved.Valid {
		link.ResolvedAt = &resolved.Time
	}
	if mediaProbed.Valid {
		link.MediaProbedAt = &mediaProbed.Time
	}
	return link, err
}

func scanP115SyncRun(rows pgx.Rows) (models.P115SyncRun, error) {
	var run models.P115SyncRun
	var ended sql.NullTime
	err := rows.Scan(&run.ID, &run.Trigger, &run.Status, &run.Mode, &run.TreeVersion, &run.Exported, &run.Generated, &run.Restored,
		&run.Updated, &run.Deleted, &run.Skipped, &run.Failed, &run.ErrorMessage, &run.EventSummary, &run.StartedAt, &ended, &run.DurationMS)
	if ended.Valid {
		run.EndedAt = &ended.Time
	}
	return run, err
}

func scanInts(rows pgx.Rows) ([]int, error) {
	values := make([]int, 0)
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func (s *Store) updateMediaStatus(ctx context.Context, id, status string, previous ...string) error {
	if len(previous) == 0 {
		return fmt.Errorf("非法媒体状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET process_status=$2, updated_at=now()
		WHERE id=$1 AND process_status=ANY($3)`, id, status, previous)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+status)
}

func previousMediaStatuses(status string) []string {
	switch status {
	case models.StatusScanned:
		return []string{models.StatusIncoming}
	case models.StatusParsed:
		return []string{models.StatusScanned}
	case models.StatusScraped:
		return []string{models.StatusParsed}
	case models.StatusMatched:
		return []string{models.StatusScraped}
	case models.StatusCollectionChecked:
		return []string{models.StatusMatched}
	case models.StatusPlanned:
		return []string{models.StatusCollectionChecked}
	case models.StatusDone, models.StatusIncompleteCollection:
		return []string{models.StatusPlanned}
	default:
		return nil
	}
}

func isTerminalBatchStatus(status string) bool {
	return status == models.BatchStatusCancelled ||
		status == models.BatchStatusComplete ||
		status == models.BatchStatusFailed ||
		status == models.BatchStatusInterrupted
}

func requireRows(rows int64, message string) error {
	if rows == 0 {
		return errors.New(message)
	}
	return nil
}
