# Curio

Curio 是一个媒体识别、重命名、归档和 STRM 播放辅助工具。它可以处理本地文件，也可以通过 CloudDrive2 处理云端文件；可以接入 TMDB 获取电影、剧集、合集信息；可以根据 YAML 分类策略和命名模板生成整理路径；也可以通过 115 生成 STRM，并提供 302 播放入口给 Emby 或播放器使用。

## 1. 基础架构

Curio 由以下服务组成：

- `backend`：Go 后端，提供 API、任务调度、识别整理、115 STRM 和 Emby 反代。
- `frontend`：React 前端，已打包进 backend 镜像，由 backend 直接托管。
- `db`：PostgreSQL，保存设置、任务、媒体文件、TMDB 元数据、剧集和合集状态。
- `redis`：保存任务队列和扫描锁，避免重复任务。
- `CloudDrive2`：可选，用于处理云端文件。
- `115`：可选，用于 STRM 生成和 302 播放。
- `Emby`：可选，用于通过 Curio 反代实现 115 STRM 302 播放。

默认访问地址：

- Curio Web：`http://localhost:8080`
- Emby 反代入口：`http://localhost:8097`

## 2. 快速启动

1. 准备 Docker 和 Docker Compose。
2. 获取项目：

```bash
git clone https://github.com/Mon3tr-v/Curio.git
cd Curio
```

3. 按需修改 `docker-compose.yml`：

- 默认镜像：`mon3trd/curio:latest`
- Web 端口：`8080`
- Emby 反代端口：`8097`
- 数据目录：`./data`
- 配置目录：`./config`
- 首次部署建议把 `CURIO_PLAY_SECRET` 改成随机长字符串。

4. 启动：

```bash
docker compose up -d
```

5. 打开：

```text
http://localhost:8080
```

6. 首次进入后先配置 `设置` 页面。

## 3. 首次配置

### 3.1 本地目录

位置：`设置 -> 基础 -> 本地目录`

- `入库目录`：Curio 扫描的本地源目录。
- `整理目录`：识别成功后移动到的目标目录。
- `失败目录`：识别或移动失败后归档目录。
- `缺失合集目录`：合集未完整时的临时归档目录。

注意：

- 四个目录不能相同。
- 输出目录不能位于入库目录内部。
- Curio 会自动创建目录并检查读写权限。

### 3.2 TMDB 与网络

位置：`设置 -> 基础 -> TMDB 与网络`

- `TMDB API Key`：用于识别电影、剧集和合集。
- `网络代理`：可填写 `http://host:port` 或 `https://host:port`。

识别标题优先级：

1. `zh-CN`
2. `zh-SG`
3. 英文

如果简体中文标题缺失或 TMDB 返回英文，会继续尝试其他语言，最终回退英文。

### 3.3 CloudDrive2

位置：`设置 -> 云端 -> CloudDrive2`

- `服务地址`：CloudDrive2 HTTP/gRPC 地址。
- `用户名 / 密码 / Token`：按 CloudDrive2 配置填写。
- `扫描根目录`：云端扫描入口。
- `整理目录 / 失败目录 / 缺失合集目录`：云端整理目标目录。

点击 `测试` 检查连接，点击 `整理云端` 启动云端整理任务。

### 3.4 115

位置：`设置 -> 115`

核心字段：

- `Cookies`：用于 115 Web API，优先用于目录树导出。
- `扫码设备`：用于扫码获取 Cookies，默认 `微信小程序`。
- `媒体库 CID`：只填写一个 115 目录 CID，Curio 只会导出或扫描这个 CID 下的目录。
- `STRM 输出目录`：生成 `.strm` 文件的位置。
- `Curio 外部地址`：播放器访问 Curio 的地址，例如 `http://192.168.10.10:8080`。

获取 Cookies：

1. 点击 `获取 Cookies`。
2. 用 115 App 扫码。
3. 手机确认后点击 `保存 Cookies`。

Open Token：

- `OpenList Access Token / Refresh Token` 可以导入。
- `OAuth 登录` 保留给你以后使用自己的 App ID / App Secret。
- Open Token 可用于 Open API 和 302 直链，但当前目录树导出优先使用 Cookies。

115 STRM 同步逻辑：

- 有 Cookies：优先调用 115 Web 的目录树导出接口，下载目录树文本后解析。
- 只有 Open Token：递归分页扫描 CID 下的完整目录。
- 每次同步都会生成当前快照，再和数据库旧记录做差异对比。
- 源目录删除或移动后，Curio 会标记或删除对应 STRM。

### 3.5 Emby 反代

位置：`设置 -> Emby`

- `Emby 原始地址`：真实 Emby 地址，例如 `http://192.168.10.83:8096`。
- `Emby 对外地址`：给播放器访问的 Curio 反代地址，例如 `http://192.168.10.83:8097`。
- `反代端口`：默认 `8097`。
- `API Key`：用于同步后刷新 Emby 媒体库。

Emby 使用方式：

1. Emby 媒体库指向 Curio 生成的 STRM 目录。
2. 播放器访问 Emby 反代端口。
3. Curio 拦截播放请求。
4. Curio 根据 STRM 链接向 115 获取直链。
5. 返回 `302` 给播放器。

媒体流量不经过 Curio 本机。

## 4. 页面说明

### 4.1 总览

显示系统状态、任务状态、最近批次和最近活动。

可查看：

- 数据库状态
- Redis 状态
- 当前任务
- 成功、失败、缺失合集数量

### 4.2 扫描

用于启动本地整理任务。

按钮：

- `开始整理`：扫描本地入库目录。
- `整理云端`：扫描 CloudDrive2 配置的云端根目录。
- `停止`：停止当前活动任务。

任务同一时间只能运行一个。刷新页面后仍会保留当前页面和任务状态。

### 4.3 处理 / 完成 / 失败

三类页面都支持：

- 搜索
- 分页
- 单选
- 全选
- 批量删除数据库记录
- 批量重新归档
- 点击行查看详情

删除只删除数据库记录，不删除真实源文件。

重新归档：

- 电影：可输入 TMDB ID，也可以留空按当前文件名重新识别。
- 剧集：可输入 TMDB ID、季、集、季偏移、集偏移。
- 失败记录和识别错误记录都可以使用重新归档。

### 4.4 剧集

剧集页面按 TMDB 剧集聚合。

详情弹窗显示：

- 季列表
- 已有集数
- 缺失集数
- 未播出集数
- 每集标题、播出日期、本地文件状态

未播出的集不会计入缺失。

### 4.5 合集

合集页面按 TMDB Collection 聚合。

详情弹窗显示：

- 合集内所有电影
- 已拥有电影
- 缺失电影
- 未上映电影

未上映电影会单独统计，不计入缺失。

### 4.6 分类

位置：`分类`

使用 YAML 配置电影和剧集分类策略。分类名也是二级目录名。

整理层级固定为：

```text
movies / 二级分类 / 片名 / 文件
tv / 二级分类 / 剧名 / Season xx / 文件
collections / 二级分类 / 合集名 / 片名 / 文件
```

示例：

```yaml
movie:
  纪录片:
    genre_ids: "99,-10402"
  欧美电影:

tv:
  国产剧集:
    origin_country: "CN,SG"
  欧美剧集:
    origin_country: "US,FR,GB,DE,ES,IT,NL,PT,RU,UK,CO"
  未分类:
```

规则说明：

- `genre_ids`：类型 ID。
- `original_language`：原始语言。
- `origin_country`：剧集国家或地区。
- `production_countries`：电影制片国家或地区。
- 值用逗号分隔。
- 负号表示排除。
- 空分类表示兜底分类。

### 4.7 命名

位置：`命名`

可配置四类模板：

- 电影
- 剧集
- 完整合集
- 缺失合集

可点击字段说明按钮查看全部字段，并点击复制。

常用字段：

```text
{title}
{year}
{category}
{resolution}
{source}
{video_codec}
{audio_codec}
{audio_channels}
{hdr_format}
{extension}
{show_title}
{show_year}
{season}
{season_2}
{episode}
{episode_2}
{episode_title}
{collection_name}
{collection_id}
```

技术字段来源：

- `{resolution}`
- `{video_codec}`
- `{audio_codec}`
- `{audio_channels}`
- `{hdr_format}`

这些字段优先通过 `ffprobe` 获取真实媒体信息，而不是从文件名猜测。模板没有使用技术字段时，Curio 会跳过不必要的 ffprobe，减少耗时。

编码规范化：

- `H265`、`x265`、`h.265` 会归一为 `HEVC`。
- `H264`、`x264`、`h.264` 会归一为 `AVC`。
- 其他编码也会尽量输出标准名称。

### 4.8 设置

设置页按顶部 Tab 分组：

- `基础`：本地目录、TMDB、网络代理。
- `云端`：CloudDrive2。
- `115`：Cookies、Open Token、CID、STRM。
- `Emby`：反代与媒体服务器。

所有需要反馈的操作都会通过右上角 Toast 显示。

## 5. 整理流程

### 5.1 本地整理

流程：

```text
扫描文件
-> 过滤非媒体资源
-> 解析文件名
-> 按需 ffprobe 获取真实技术参数
-> TMDB 识别
-> 分类 YAML 匹配
-> 命名模板生成目标路径
-> 移动媒体文件
-> 移动字幕
-> 删除空文件夹
-> 更新数据库状态
```

非媒体文件不会进入状态机。

### 5.2 云端整理

流程和本地整理相同，但文件操作通过 CloudDrive2 完成。

云端整理会：

- 识别 CloudDrive2 路径。
- 移动云端媒体文件。
- 同步移动字幕。
- 清理空目录。

### 5.3 字幕处理

Curio 会移动同目录下的字幕文件。

字幕语言后缀：

- 简体中文字幕：`.chs`
- 繁体中文字幕：`.cht`
- 其他语言尽量保留或识别为对应后缀

示例：

```text
Movie.mkv
Movie.zh-CN.srt
```

整理后：

```text
电影名 (2024) - 2160p HEVC.mkv
电影名 (2024) - 2160p HEVC.chs.srt
```

## 6. 115 STRM 与 302 播放

### 6.1 STRM 生成

点击 `设置 -> 115 -> 同步 STRM` 后：

1. Curio 获取配置 CID 的目录树。
2. 过滤媒体扩展名。
3. 为每个媒体生成 STRM。
4. 写入数据库 `strm_links`。
5. 删除或标记已不存在的 STRM。

STRM 内容示例：

```text
http://localhost:8080/play/115/电影名.iso?token=签名token
```

路径显示真实媒体名，鉴权 token 放在 query 参数里。

### 6.2 302 播放

播放器访问 STRM 后：

1. Curio 校验签名 token。
2. 根据 STRM 记录找到 115 文件。
3. 使用播放器 User-Agent 向 115 获取直链。
4. 返回 302。

媒体流量由播放器直连 115，不经过 Curio。

### 6.3 目录树导出与扫描

- 配置 Cookies：优先下载 115 导出的目录树文本。
- 只有 Open Token：递归扫描 CID 下的完整目录。

推荐配置 Cookies，减少 API 请求和限流概率。

## 7. 常见问题

### 7.1 115 请求达到访问上限

这是 115 服务端限流。常见原因：

- 连续同步 STRM。
- Emby 扫描大量 STRM。
- 播放器批量探测。
- Open Token 模式递归扫描大目录。

处理方式：

- 暂停 Emby 扫描。
- 等待一段时间后重试。
- 使用 Cookies 目录树导出。
- 减少频繁点击同步。

### 7.2 扫码状态刷新很慢

115 状态接口可能会 long-poll，等待 20 到 30 秒是正常现象。

### 7.3 识别错误

在 `处理 / 完成 / 失败` 页面打开详情，使用 `重新归档`：

- 电影可填写 TMDB ID。
- 剧集可填写 TMDB ID、季、集和偏移。
- 留空则按当前文件名重新识别。

### 7.4 页面只显示部分记录

列表采用分页加载，不会一次性加载全量数据。使用搜索和翻页查看更多记录。

### 7.5 Cookie 是否永久有效

不会永久有效。扫码获取的是较稳定的 115 登录态，但仍可能因服务端策略、IP、设备管理或账号安全策略失效。失效后重新扫码即可。

## 8. Docker 镜像

本项目镜像包含：

- Go backend
- 前端 dist
- ffmpeg / ffprobe
- CA 证书

默认 Compose 使用镜像：

```text
mon3trd/curio:latest
```

使用仓库内的 `docker-compose.yml` 运行：

```bash
docker compose up -d
```

默认会启动：

- `db`：PostgreSQL 17，保存 Curio 数据。
- `redis`：Redis 7，开启 AOF，并限制缓存内存为 `192mb`。
- `curio`：Curio 主服务，对外暴露 `8080` 和 `8097`。

默认挂载：

- `./data:/data/Curio`
- `./config:/config`
- `curio_postgres:/var/lib/postgresql/data`
- `curio_redis:/data`

如果 Curio 需要访问宿主机代理，可以在页面设置中填写：

```text
http://host.docker.internal:7890
```

或单独运行 Curio 主服务：

```bash
docker run -d \
  --name curio \
  -p 8080:8080 \
  -p 8097:8097 \
  -e SERVER_ADDR=:8080 \
  -e DATABASE_URL='postgres://curio:curio@db:5432/curio?sslmode=disable' \
  -e REDIS_ADDR='redis:6379' \
  -e FRONTEND_DIR=/app/public \
  -e CURIO_DATA_ROOT=/data/Curio \
  -e CURIO_PLAY_SECRET='change-me' \
  -v ./data:/data/Curio \
  -v ./config:/config \
  mon3trd/curio:latest
```

单独运行时仍需要准备可访问的 PostgreSQL 和 Redis。

## 9. 安全建议

- 不要公开 TMDB Key、115 Cookies、Open Token、Emby API Key。
- `CURIO_PLAY_SECRET` 应设置为随机长字符串。
- 115 播放 URL 中的 `token` 有播放权限，不要公开分享。
- 反代端口只暴露给可信网络或通过网关鉴权。
