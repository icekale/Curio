package config

const DefaultAIFilenamePrompt = `You are Curio's media filename parser. Analyze only the provided file name and relative path. Do not search the internet and do not invent TMDB IDs.

Return one JSON object with an "items" array. Each item must keep the input index and describe the best filename interpretation for later TMDB search.

Rules:
- media_type must be one of: movie, tv_episode, unknown.
- Put unknown or uncertain fields as null or empty strings.
- Do not treat 2in1, Extended, Theatrical, Director's Cut, Special Edition, IMAX, Remastered, Criterion, Open Matte, Complete, BDMV, BluRay, Remux, DTS-HD, TrueHD, Atmos, AVC, HEVC, x264, x265, 1080p, 2160p as episode numbers.
- ISO and BDMV names are usually movies unless SxxEyy, 1x02, EP02, Chinese episode markers, or a season directory clearly says they are TV episodes.
- Use SxxEyy, 1x02, Episode 02, EP02, Chinese episode markers, and season directories to identify TV episodes.
- Preserve release metadata such as version, edition, source, resolution, video codec, audio codec, audio channels, HDR format, and release group.
- confidence is 0 to 1. Set needs_review=true when confidence is below 0.75 or when title/type/year/season/episode are ambiguous.
- Never return tmdb_id. Curio will search TMDB itself.`

const DefaultClassificationYAML = `movie:
  纪录片:
    genre_ids: "99,-10402"
  演唱会:
    genre_ids: "10402"
  动画电影:
    genre_ids: "16"
  华语电影:
    original_language: "zh,cn,bo,za"
  日韩电影:
    original_language: "ja,ko,th"
  欧美电影:

tv:
  国漫:
    genre_ids: "16"
    origin_country: "CN,TW,HK"
  日漫:
    genre_ids: "16"
    origin_country: "JP"
  欧美动漫:
    genre_ids: "16"
    origin_country: "US,FR,GB,DE,ES,IT,NL,PT,RU,UK"
  儿童动漫:
    genre_ids: "10762"
  其他动漫:
    genre_ids: "16"
  纪录片:
    genre_ids: "99"
  综艺:
    genre_ids: "10764,10767"
  国产剧集:
    origin_country: "CN,SG"
  香港剧集:
    origin_country: "HK"
  台湾剧集:
    origin_country: "TW"
  日本剧集:
    origin_country: "JP"
  韩国剧集:
    origin_country: "KP,KR,TH,IN"
  欧美剧集:
    origin_country: "US,FR,GB,DE,ES,IT,NL,PT,RU,UK,CO"
  未分类:
`
