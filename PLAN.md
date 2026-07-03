# PLAN

goup のロードマップとタスク進捗を記録する。完了タスクは削除せず `[x]` でマークして履歴として残す。

## 現状の把握 (v0.1.0 時点)

- 2026-07-03: **v0.1.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.1.0))
  - サブコマンド: `check` / `update` / `rollback` / `help`
  - `/usr/local/go` の入れ替え、sha256 検証、1 世代バックアップ、自動 rollback
  - fast-fail 権限チェック、sudo PATH 剥奪対応（VERSION ファイル直読み）
  - 配布バイナリ: linux/amd64, darwin/arm64

## v0.2.0: 指定バージョンインストール + list

「常に最新に追従」だけでなく、特定バージョンをピン留めしたいユースケースに対応する。goupdate（既存 zsh 関数）が果たしていた「1.23.0 だけ入れたい」を goup に取り込む。

### `goup install <version>`

- [ ] コマンド追加。`goup install 1.26.3` / `goup install go1.26.3` の両方を受ける
- [ ] バリデーション:
  - [ ] `FetchReleases` の結果に version が存在するか確認（存在しなければエラー、`goup list` を案内）
  - [ ] 非 stable（beta/rc）は `--pre` フラグ無しでは弾く（デフォルト stable のみ）
- [ ] インストール処理:
  - [ ] 既存の `Update` 内部を再利用（`LatestArchive` の代わりに `FindArchive(releases, wantVersion, goos, goarch)` を切り出す）
  - [ ] 現在バージョンと同一なら "Already at <version>" で早期 return（update と同挙動）
  - [ ] ダウングレード時は明示的に告知（例: `Installing go1.24.5 (downgrading from go1.26.3) ...`）
- [ ] fast-fail 権限チェック・sha256 検証・バックアップ・自動 rollback は既存経路を流用
- [ ] テスト:
  - [ ] 存在しないバージョンでエラー
  - [ ] beta/rc は `--pre` 無しで弾かれる
  - [ ] ダウングレードが動く（バックアップは正しく作られる）
  - [ ] 既に同一バージョンなら no-op

### `goup list`

- [ ] コマンド追加。既定は「直近の stable 版を新しい順で数件」表示
- [ ] 出力フォーマット案:
  ```
    go1.26.4  (stable)  <- current
    go1.26.3  (stable)
    go1.26.2  (stable)
    go1.26.1  (stable)
    go1.26.0  (stable)
  ```
  - 現在バージョン（`CurrentVersion(installRoot)` の返り値）に矢印を付ける
  - 現在バージョンが list 範囲外なら末尾に別行で表示
- [ ] フラグ:
  - [ ] `--all`: 全 release（beta/rc/歴代 stable）を表示
  - [ ] `--pre`: stable + pre-release のみ
  - [ ] `-n <N>`: 表示件数上限（既定 5〜10）
- [ ] テスト:
  - [ ] `httptest` で複数リリース（stable + beta 混在）を用意して既定表示が stable のみか
  - [ ] `--all` で beta も含まれるか
  - [ ] 現在バージョンのハイライト

### `goup version` / `--version`

リリース済みバイナリで「どの版を今使っているか」を確認する手段が無い。v0.2.0 で導入する。

- [ ] `main.go` に `var version = "dev"` を追加し、リリースビルドで `-ldflags="-X main.version=v0.2.0"` を渡してタグを埋め込む
- [ ] `goup version` / `goup --version` / `goup -v` で表示。フォーマット案:
  ```
  goup v0.2.0 (linux/amd64, go1.26.4)
  ```
  ビルド OS/Arch と、ビルド時の Go バージョン (`runtime.Version()`) も出す
- [ ] リリースワークフロー（`/pr-release-claude` 経由の手順）に `-ldflags` を組み込む。ビルドコマンドの正解を README か CLAUDE.md に残す
- [ ] テスト: 埋め込み無し（`dev`）とタグ付き両方で `goup version` の出力形状を軽く検証

### 副次の整理（着手時に検討）

- [ ] `main.go` の switch 文が肥大化してくるので、サブコマンド dispatch を薄い map か slice ベースに整理するか検討（フレームワーク非導入方針は維持）

## スコープ外（追加しない機能）

goup のミッションは **「/usr/local/go を安全に入れ替える 1 点」** に絞る。以下は意図的に取り込まない。要望が来たら「他のツールで解決してください」と案内する立場を維持する。

| 機能 | やらない理由 | 代替 |
|---|---|---|
| 複数バージョンの並行管理・切替 | mission creep。gopath/GOROOT の抽象を持ち込むと単一静的バイナリ・stdlib-only の身軽さが崩壊する | `mise` / `asdf` / `gvm` |
| プロジェクトごとのバージョンピン | 上と同じ。`go.mod` の `toolchain` directive が既にその責務を負っている | `go.mod` の `toolchain` |
| Windows ネイティブ対応 | 対象 OS を絞ることで install path・パーミッションモデル・tarball 形式の仮定を単純化できている | `winget` / Chocolatey |
| Config ファイル / 環境変数での挙動変更 | 4〜7 コマンドの CLI に設定機構は YAGNI。すべての挙動は引数と実行結果で自己完結させる | — |
| バックアップ多世代化 / `goup gc` | 単一世代の自動保持で 99% のロールバック要求を満たす。多世代 = 複雑性 & ディスク食い | 手動で `sudo mv /usr/local/go /somewhere` |
| 初回セットアップ (`/usr/local/go` が無い環境への新規インストール) | インストーラは既に公式手順が単純（`curl | tar`）。二重化する価値が薄い | 公式ドキュメント |
| 自動アップデート (cron / systemd timer) | 「明示的にユーザーが叩く」設計を崩さない。副作用のあるツールを常駐化させない | ユーザー側の cron 設定 |

**この一覧は縮小できるが、拡張は慎重に。** 新機能を検討する際は必ずこの表を見返して「本当に goup の責任か」を問うこと。
