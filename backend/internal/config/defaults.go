package config

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
