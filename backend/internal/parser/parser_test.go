package parser

import (
	"strings"
	"testing"
)

func TestParseMovie(t *testing.T) {
	result, err := Parse("Avatar.2009.1080p.BluRay.x264.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Avatar" || result.Year != 2009 || result.Resolution != "1080p" || result.Source != "BluRay" || result.VideoCodec != "AVC" {
		t.Fatalf("unexpected movie parse: %+v", result)
	}
}

func TestParseTVEpisode(t *testing.T) {
	result, err := Parse("The.Last.of.Us.S01E03.1080p.WEB-DL.x265.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Last of Us" || result.Season != 1 || result.Episode != 3 || result.Season2 != "01" || result.Episode2 != "03" {
		t.Fatalf("unexpected episode parse: %+v", result)
	}
}

func TestParseTVEpisodeAlternateFormat(t *testing.T) {
	result, err := Parse("Show.Name.1x03.720p.HDTV.H264.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Show Name" || result.Season != 1 || result.Episode != 3 {
		t.Fatalf("unexpected alternate episode parse: %+v", result)
	}
}

func TestParseTVEpisodeFromSeasonFolder(t *testing.T) {
	result, err := ParsePath("/media/The Last of Us (2023)/Season 1/E03.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Last of Us" || result.ShowYear != 2023 || result.Season != 1 || result.Episode != 3 {
		t.Fatalf("unexpected path episode parse: %+v", result)
	}
}

func TestParseTVEpisodeFromNamedSeasonFolder(t *testing.T) {
	result, err := ParsePath("/media/了不起的麦瑟尔夫人.S01-S03.2017-2019.1080p.WEB-DL/了不起的麦瑟尔夫人.S03.2019.1080p.WEB-DL/The.Marvelous.Mrs.Maisel.S03E01.2019.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Marvelous Mrs Maisel" || result.ShowYear != 2017 || result.Season != 3 || result.Episode != 1 {
		t.Fatalf("unexpected named season folder parse: %+v", result)
	}
}

func TestParseTVEpisodeUsesSeriesYearFromSeasonRangeParent(t *testing.T) {
	result, err := ParsePath("/media/Dark.S01-S03.2017-2020.1080p.WEB-DL/Dark.S02.2019.1080p.WEB-DL/Dark.S02E01.2019.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Dark" || result.ShowYear != 2017 || result.Season != 2 || result.Episode != 1 {
		t.Fatalf("unexpected dark season parse: %+v", result)
	}
}

func TestParseChineseEpisode(t *testing.T) {
	result, err := Parse("庆余年.第2季.第03集.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "庆余年" || result.Season != 2 || result.Episode != 3 {
		t.Fatalf("unexpected chinese episode parse: %+v", result)
	}
}

func TestParseMovieWithTMDBIDFromFolder(t *testing.T) {
	result, err := ParsePath("/media/长安三万里 (2023) [tmdbid=1110037]/长安三万里.2160p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "长安三万里" || result.Year != 2023 || result.TMDBID != 1110037 {
		t.Fatalf("unexpected tmdb movie parse: %+v", result)
	}
}

func TestParseMovieWithoutYear(t *testing.T) {
	result, err := Parse("Inception.1080p.BluRay.x264.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Inception" || result.Year != 0 {
		t.Fatalf("unexpected no-year movie parse: %+v", result)
	}
}

func TestParseFGTFolderMovie(t *testing.T) {
	result, err := ParsePath("/115open/2267部2160p remux FGT/0[007.大破天幕杀机]Skyfall.2012.2160p.BluRay.REMUX.HEVC.DTS-HD.MA.5.1-FGT/Skyfall.2012.2160p.BluRay.REMUX.HEVC.DTS-HD.MA.5.1-FGT.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Skyfall" || result.Year != 2012 {
		t.Fatalf("unexpected fgt movie parse: %+v", result)
	}
}

func TestParseSGNBMovie(t *testing.T) {
	result, err := Parse("001.异形：契约／异形：圣约(港／台) [SGNB首部UHD原盘DIY BDJ菜单修改 次时代国语音轨 简繁英特效四字幕]Alien Covenant 2017 ULTRAHD Blu-ray 2160p HEVC Atoms TrueHD 7.1-SGnb@CHDBits.iso")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Alien Covenant" || result.Year != 2017 {
		t.Fatalf("unexpected sgnb movie parse: %+v", result)
	}
}

func TestParseSGNBMovieWithYearInChineseTitle(t *testing.T) {
	result, err := Parse("224.神奇女侠1984 [SGNB第224部UHD原盘中文字幕DIY]Wonder Woman 1984 2020 ULTRAHD BluRay 2160p HEVC Atmos TrueHD 7.1-sGnb@CHDBits.iso")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Wonder Woman 1984" || result.Year != 2020 {
		t.Fatalf("unexpected sgnb year-in-title parse: %+v", result)
	}
}

func TestParseNyaaEpisode(t *testing.T) {
	result, err := Parse("[ANi] Kusuriya no Hitorigoto - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Kusuriya no Hitorigoto" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected nyaa episode parse: %+v", result)
	}
}

func TestParseNyaaChineseGroupEpisode(t *testing.T) {
	result, err := Parse("[喵萌奶茶屋&LoliHouse] 葬送的芙莉莲 - 05 [WebRip 1080p HEVC-10bit AAC][简繁日内封字幕].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "葬送的芙莉莲" || result.Season != 1 || result.Episode != 5 {
		t.Fatalf("unexpected nyaa chinese group episode parse: %+v", result)
	}
}

func TestParseCloudRootDoesNotUseIncomingAsShowTitle(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[Nekomoe kissaten&VCB-Studio] Youkoso Jitsuryoku Shijou Shugi no Kyoushitsu e [05][Ma10p_1080p][x265_flac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Youkoso Jitsuryoku Shijou Shugi no Kyoushitsu e" || result.Season != 1 || result.Episode != 5 {
		t.Fatalf("unexpected incoming-root anime parse: %+v", result)
	}
	for _, title := range result.SearchTitles {
		if title == "Incoming" || title == "incoming" {
			t.Fatalf("incoming directory leaked into search titles: %+v", result.SearchTitles)
		}
	}
}

func TestParseBroadcastPrefixedChineseTVTitle(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[中国广电重温经典频道 虹猫蓝兔七侠传].CWJDTV.Legend.of.Howie,Landau.and.the.Seven.Swordsmen.2006.S01.Complete.1080i.HDTV.AVC.DD5.1-QHstudIo/[中国广电重温经典频道 虹猫蓝兔七侠传].CWJDTV.Legend.of.Howie,Landau.and.the.Seven.Swordsmen.2006.S01E01.1080i.HDTV.AVC.DD5.1-QHstudIo.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "虹猫蓝兔七侠传" || result.ShowYear != 2006 || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected broadcast-prefixed tv parse: %+v", result)
	}
	if len(result.SearchTitles) == 0 || result.SearchTitles[0] != "虹猫蓝兔七侠传" {
		t.Fatalf("expected exact chinese title first, got %+v", result.SearchTitles)
	}
}

func TestParseAnimeAbbreviationSeasonSuffix(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[DMG&VCB-Studio] Youzitsu2 [01][Ma10p_1080p][x265_flac_aac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Youzitsu" || result.Season != 2 || result.Episode != 1 {
		t.Fatalf("unexpected anime season suffix parse: %+v", result)
	}
}

func TestParseTHXVCBReleaseGroup(t *testing.T) {
	result, err := Parse("[T.H.X&VCB-Studio] Hyouka [01][Ma10p_1080p][x265_flac_aac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Hyouka" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected thx vcb parse: %+v", result)
	}
}

func TestParseUnknownAnimeReleaseGroup(t *testing.T) {
	result, err := Parse("[CoolSub] Hyouka - 01 [1080p][AAC].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Hyouka" || result.ReleaseGroup != "CoolSub" || result.Episode != 1 {
		t.Fatalf("unexpected unknown group anime parse: %+v", result)
	}
}

func TestParseMultiEpisode(t *testing.T) {
	result, err := Parse("Stargate.Atlantis.S01E01E02.2004.Blu-ray.x265.AC3.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Stargate Atlantis" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected multi episode parse: %+v", result)
	}
	if len(result.Episodes) != 2 || result.Episodes[0] != 1 || result.Episodes[1] != 2 {
		t.Fatalf("expected multi episode list, got %+v", result.Episodes)
	}
}

func TestParseBareEpisodeFromSeasonFolder(t *testing.T) {
	parts := splitPath("/media/神盾局特工S01-S05.Marvels.Agents.of.S.H.I.E.L.D.2013-2017/神盾局特工S01.Marvels.Agents.of.S.H.I.E.L.D.2013/07.mkv")
	if season := seasonFromParts(parts); season != 1 {
		t.Fatalf("unexpected nearest season: %d rangeParent=%v singleParent=%v parts=%v", season, seasonRangeRE.MatchString(parts[2]), singleSeasonRE.FindStringSubmatch(parts[2]), parts)
	}
	result, err := ParsePath("/media/神盾局特工S01-S05.Marvels.Agents.of.S.H.I.E.L.D.2013-2017/神盾局特工S01.Marvels.Agents.of.S.H.I.E.L.D.2013/07.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.Season != 1 || result.Episode != 7 || result.ShowTitle != "Marvels Agents of S H I E L D" {
		t.Fatalf("unexpected bare episode parse: %+v", result)
	}
}

func TestParseSeasonNumberAsEpisode(t *testing.T) {
	result, err := ParsePath("/media/难以置信.Unbelievable.S01/Unbelievable.S06.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Unbelievable" || result.Season != 1 || result.Episode != 6 {
		t.Fatalf("unexpected season-as-episode parse: %+v", result)
	}
}

func TestParseMiniseriesPart(t *testing.T) {
	result, err := Parse("Battlestar.Galactica.Miniseries.Part.2.2003.1080p.Blu-ray.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Battlestar Galactica" || result.Season != 1 || result.Episode != 2 {
		t.Fatalf("unexpected miniseries parse: %+v", result)
	}
}

func TestParseEpisodeZero(t *testing.T) {
	result, err := Parse("Sherlock.S01E00.2010.1080p.Blu-ray.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Sherlock" || result.Season != 1 || result.Episode != 0 || result.Episode2 != "00" {
		t.Fatalf("unexpected episode zero parse: %+v", result)
	}
}

func TestParseHaloForwardUntoDawn(t *testing.T) {
	result, err := Parse("Halo.4.Forward.Unto.Dawn.2012.2160p.BluRay.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Halo 4 Forward Unto Dawn" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected halo parse: %+v", result)
	}
}

func TestParseMovieReleaseTokens(t *testing.T) {
	result, err := Parse("Blade.Runner.2049.2017.2160p.UHD.BluRay.REMUX.HEVC.TrueHD.7.1.Atmos.DV.HDR10-TERMiNAL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Blade Runner 2049" || result.Year != 2017 {
		t.Fatalf("unexpected release token movie parse: %+v", result)
	}
	if result.Resolution != "2160p" || result.VideoCodec != "HEVC" || result.AudioCodec != "TrueHD" || result.AudioChannels != "7.1" || result.ReleaseGroup != "TERMiNAL" {
		t.Fatalf("unexpected release tokens: %+v", result)
	}
	if result.HDRFormat != "DV HDR10" {
		t.Fatalf("unexpected hdr token %q", result.HDRFormat)
	}
}

func TestParseMovieKeepsReleaseYearBeforeEditionYears(t *testing.T) {
	tests := []struct {
		name  string
		title string
		year  int
	}{
		{
			name:  "Alien.3.1992.Theatrical.Version.2003.Special.Edition.2in1.1080p.BluRay.AVC.DTS-HD.MA5.1-NGB.iso",
			title: "Alien 3",
			year:  1992,
		},
		{
			name:  "Alien.Resurrection.1997.Theatrical.Version.2003.Special.Edition.2in1.1080p.BluRay.AVC.DTS-HD.MA5.1-NGB.iso",
			title: "Alien Resurrection",
			year:  1997,
		},
	}
	for _, tt := range tests {
		result, err := Parse(tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsTV || result.Title != tt.title || result.Year != tt.year {
			t.Fatalf("unexpected parse for %s: %+v", tt.name, result)
		}
		if result.AudioCodec != "DTS-HD MA" || result.AudioChannels != "5.1" || result.ReleaseGroup != "NGB" {
			t.Fatalf("unexpected release tokens for %s: %+v", tt.name, result)
		}
		if !strings.Contains(result.Edition, "Theatrical Version") || !strings.Contains(result.Edition, "Special Edition") || !strings.Contains(result.Edition, "2in1") {
			t.Fatalf("unexpected edition tokens for %s: %q", tt.name, result.Edition)
		}
	}
}

func TestParseMovieKeepsTitleYearWhenFollowedByReleaseYear(t *testing.T) {
	result, err := Parse("Wonder.Woman.1984.2020.2160p.BluRay.HEVC.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Wonder Woman 1984" || result.Year != 2020 {
		t.Fatalf("unexpected title-year parse: %+v", result)
	}
}

func TestParseKodiStyleTMDBID(t *testing.T) {
	result, err := ParsePath("/media/Dune Part Two (2024) {tmdb=693134}/Dune.Part.Two.2024.2160p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.TMDBID != 693134 || result.Title != "Dune Part Two" || result.Year != 2024 {
		t.Fatalf("unexpected kodi id parse: %+v", result)
	}
}

func TestParseAnimeVersionAndChecksum(t *testing.T) {
	result, err := Parse("[SubsPlease] Sousou no Frieren - 01v2 [1080p][AAC][83A1B2C3].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Sousou no Frieren" || result.Episode != 1 || result.ReleaseVersion != 2 {
		t.Fatalf("unexpected anime version parse: %+v", result)
	}
	if result.ReleaseGroup != "SubsPlease" || result.Checksum != "83A1B2C3" || result.Resolution != "1080p" || result.AudioCodec != "AAC" {
		t.Fatalf("unexpected anime tokens: %+v", result)
	}
}

func TestParseEpisodeRange(t *testing.T) {
	result, err := Parse("Show.Name.S01E01-E04.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Show Name" || result.Episode != 1 {
		t.Fatalf("unexpected range parse: %+v", result)
	}
	want := []int{1, 2, 3, 4}
	if len(result.Episodes) != len(want) {
		t.Fatalf("expected episodes %v, got %v", want, result.Episodes)
	}
	for i := range want {
		if result.Episodes[i] != want[i] {
			t.Fatalf("expected episodes %v, got %v", want, result.Episodes)
		}
	}
}

func TestParseDateBasedTVEpisode(t *testing.T) {
	result, err := Parse("The.Daily.Show.2024.05.17.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Daily Show" || result.AirDate != "2024-05-17" {
		t.Fatalf("unexpected dated tv parse: %+v", result)
	}
	if result.Season != 0 || result.Episode != 0 || result.Season2 != "" || result.Episode2 != "" {
		t.Fatalf("expected dated episode without synthetic season/episode, got %+v", result)
	}
	if result.ReleaseGroup != "" {
		t.Fatalf("expected WEB-DL not to leak release group, got %+v", result)
	}
	for _, title := range result.SearchTitles {
		if strings.Contains(title, "2024") || strings.Contains(title, "05 17") {
			t.Fatalf("expected dated tokens stripped from search titles, got %+v", result.SearchTitles)
		}
	}
}

func TestParseGuessItEpisodePatterns(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		season   int
		episode  int
		episodes []int
		airDate  string
		group    string
	}{
		{
			name:    "Show.Name.102.720p.HDTV.x264-GROUP.mkv",
			title:   "Show Name",
			season:  1,
			episode: 2,
			group:   "GROUP",
		},
		{
			name:    "The.100.101.HDTV.x264.mkv",
			title:   "The 100",
			season:  1,
			episode: 1,
		},
		{
			name:    "Show.Name.Season.1.Episode.2.1080p.WEB-DL.mkv",
			title:   "Show Name",
			season:  1,
			episode: 2,
		},
		{
			name:     "Show.Name.S01E01+E02.1080p.WEB-DL.mkv",
			title:    "Show Name",
			season:   1,
			episode:  1,
			episodes: []int{1, 2},
		},
		{
			name:    "Show.Name.2024-5-7.1080p.WEB-DL.mkv",
			title:   "Show Name",
			airDate: "2024-05-07",
		},
	}
	for _, tt := range tests {
		result, err := Parse(tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsTV || result.ShowTitle != tt.title || result.Season != tt.season || result.Episode != tt.episode || result.AirDate != tt.airDate {
			t.Fatalf("unexpected GuessIt-style parse for %s: %+v", tt.name, result)
		}
		if tt.group != "" && result.ReleaseGroup != tt.group {
			t.Fatalf("expected release group %q for %s, got %+v", tt.group, tt.name, result)
		}
		if len(tt.episodes) > 0 {
			if len(result.Episodes) != len(tt.episodes) {
				t.Fatalf("expected episodes %v for %s, got %v", tt.episodes, tt.name, result.Episodes)
			}
			for i := range tt.episodes {
				if result.Episodes[i] != tt.episodes[i] {
					t.Fatalf("expected episodes %v for %s, got %v", tt.episodes, tt.name, result.Episodes)
				}
			}
		}
	}
}

func TestParseGuessItAudioProfile(t *testing.T) {
	result, err := Parse("Movie.Name.2024.2160p.UHD.BluRay.DTS-HD.HR.7.1.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Movie Name" || result.Year != 2024 {
		t.Fatalf("unexpected movie parse: %+v", result)
	}
	if result.AudioCodec != "DTS-HD" || result.AudioProfile != "High Resolution Audio" || result.AudioChannels != "7.1" {
		t.Fatalf("unexpected DTS-HD HR parse: %+v", result)
	}
}

func TestParseIMDbTagStripsSearchNoise(t *testing.T) {
	result, err := ParsePath("/media/Dune Part Two (2024) {imdb=tt15239678}/Dune.Part.Two.2024.2160p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.IMDbID != "tt15239678" || result.Title != "Dune Part Two" || result.Year != 2024 {
		t.Fatalf("unexpected imdb-tag parse: %+v", result)
	}
	for _, title := range result.SearchTitles {
		if strings.Contains(strings.ToLower(title), "imdb") || strings.Contains(strings.ToLower(title), "tt15239678") {
			t.Fatalf("expected imdb tag stripped from search titles, got %+v", result.SearchTitles)
		}
	}
}

func TestParseCompactAudioCodecAndAltResolution(t *testing.T) {
	result, err := Parse("Deadliest.Catch.S00E66.No.Safe.Passage.720p.AMZN.WEB-DL.DDP2.0.H.264-NTb[TGx].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Deadliest Catch" || result.Season != 0 || result.Episode != 66 {
		t.Fatalf("unexpected compact audio tv parse: %+v", result)
	}
	if result.AudioCodec != "DDP" || result.AudioChannels != "2.0" || result.VideoCodec != "AVC" {
		t.Fatalf("unexpected compact audio tokens: %+v", result)
	}
}

func TestParseAnimeYearDropsOutOfTitleAndAltResolution(t *testing.T) {
	result, err := Parse("[TaigaSubs] Toradora! (2008) - 01v2 - Tiger and Dragon [1280x720 H.264 FLAC][1234ABCD].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Toradora!" || result.Episode != 1 || result.ReleaseVersion != 2 {
		t.Fatalf("unexpected anime year parse: %+v", result)
	}
	if result.Resolution != "720p" || result.VideoCodec != "AVC" || result.AudioCodec != "FLAC" || result.Checksum != "1234ABCD" {
		t.Fatalf("unexpected anime release tokens: %+v", result)
	}
}

func TestParseRichEpisodeProperties(t *testing.T) {
	result, err := Parse("[SubsPlease] Example Show - 01 - A New Start [1080p][AAC][ENG][CHS].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Example Show" || result.Episode != 1 {
		t.Fatalf("unexpected rich episode parse: %+v", result)
	}
	if result.EpisodeTitle != "A New Start" {
		t.Fatalf("expected episode title, got %+v", result)
	}
	if len(result.Languages) != 1 || result.Languages[0] != "English" {
		t.Fatalf("expected English audio language, got %+v", result.Languages)
	}
	if len(result.SubtitleLanguages) != 1 || result.SubtitleLanguages[0] != "Chinese (Simplified)" {
		t.Fatalf("expected simplified Chinese subtitles, got %+v", result.SubtitleLanguages)
	}
	if result.Container != "mkv" || result.MimeType == "" {
		t.Fatalf("expected container and mimetype, got %+v", result)
	}
	if len(result.Elements) == 0 {
		t.Fatalf("expected structured elements, got %+v", result)
	}
}

func TestParseStreamingServiceAndWebsite(t *testing.T) {
	result, err := Parse("Show.Name.S01E01.Pilot.1080p.NF.WEB-DL.x264.www.example.com.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Show Name" || result.Episode != 1 {
		t.Fatalf("unexpected streaming-service parse: %+v", result)
	}
	if result.StreamingService != "Netflix" {
		t.Fatalf("expected Netflix streaming service, got %+v", result)
	}
	if result.Website != "www.example.com" {
		t.Fatalf("expected website, got %+v", result)
	}
	if result.EpisodeTitle != "Pilot" {
		t.Fatalf("expected pilot episode title, got %+v", result)
	}
}

func TestParseRichTechnicalAndPackFields(t *testing.T) {
	result, err := Parse("Show.Name.S01-S03.S02E01-E04.Part.2.1440p.WEB-DL.H265.Main10.10bit.23.976fps.1.5Mbps.2.1GB.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.Season != 2 || result.Episode != 1 {
		t.Fatalf("unexpected pack parse: %+v", result)
	}
	if result.SeasonCount != 3 || result.EpisodeCount != 4 || result.Part != 2 {
		t.Fatalf("expected season/episode count and part, got %+v", result)
	}
	if result.Resolution != "1440p" || result.VideoCodec != "HEVC" || result.VideoProfile != "Main 10" {
		t.Fatalf("expected rich video fields, got %+v", result)
	}
	if result.ColorDepth != "10-bit" || result.FrameRate != "23.976fps" || result.VideoBitRate != "1.5Mbps" || result.Size != "2.1GB" {
		t.Fatalf("expected extra technical metadata, got %+v", result)
	}
	wantEpisodes := []int{1, 2, 3, 4}
	if len(result.Episodes) != len(wantEpisodes) {
		t.Fatalf("expected episodes %v, got %v", wantEpisodes, result.Episodes)
	}
	for i := range wantEpisodes {
		if result.Episodes[i] != wantEpisodes[i] {
			t.Fatalf("expected episodes %v, got %v", wantEpisodes, result.Episodes)
		}
	}
}
