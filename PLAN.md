# PLAN

goup のロードマップとタスク進捗を記録する。完了タスクは削除せず `[x]` でマークして履歴として残す。

## 現状の把握 (v0.2.0 実装完了、リリース前)

- 2026-07-03: **v0.1.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.1.0))
  - サブコマンド: `check` / `update` / `rollback` / `help`
  - `/usr/local/go` の入れ替え、sha256 検証、1 世代バックアップ、自動 rollback
  - fast-fail 権限チェック、sudo PATH 剥奪対応（VERSION ファイル直読み）
  - 配布バイナリ: linux/amd64, darwin/arm64
- 2026-07-03: **v0.2.0 の 3 機能実装完了（branch `feat/v0.2.0`）**
  - `goup version` / `--version` — `-ldflags "-X main.version=..."` で埋め込み
  - `goup list [--all] [-n N]` — 現在バージョンを `*` でハイライト、window 外なら省略記号で追記
  - `goup install <version> [--pre]` — Update と共通の installArchive 経路を使用
  - リリースビルドコマンドを `CLAUDE.md` に追記

## v0.2.0: 指定バージョンインストール + list（実装完了・リリース待ち）

「常に最新に追従」だけでなく、特定バージョンをピン留めしたいユースケースに対応する。goupdate（既存 zsh 関数）が果たしていた「1.23.0 だけ入れたい」を goup に取り込む。

### `goup install <version>` （完了）

- [x] コマンド追加。`goup install 1.26.3` / `goup install go1.26.3` の両方を受ける
  > `NormalizeVersion` で先頭 `go` を補完し、`FindArchive` で厳密一致検索
- [x] バリデーション:
  - [x] version が存在するか確認（存在しなければエラー、`goup list --all` を案内）
  - [x] 非 stable（beta/rc）は `--pre` フラグ無しでは弾く
- [x] インストール処理:
  - [x] 既存の `Update` 内部を再利用（`installArchive` を切り出し、`FindArchive` を追加）
  - [x] 現在バージョンと同一なら "Already at <version>" で早期 return
  - [x] ダウングレード時は明示的に告知
    > `Installed: goX (was goY)` の一律メッセージで上げも下げも同じ形。専用の "downgrading" 語彙は不要と判断
- [x] fast-fail 権限チェック・sha256 検証・バックアップ・自動 rollback は既存経路を流用
- [x] テスト:
  - [x] 存在しないバージョンでエラー
  - [x] beta/rc は `--pre` 無しで弾かれる（`TestInstall_PreReleaseRefusedWithoutFlag`）
  - [x] `--pre` で pre-release がインストールできる（`TestInstall_PreReleaseAcceptedWithFlag`）
  - [x] 既に同一バージョンなら no-op（`TestInstall_AlreadyAtTarget`、バックアップも作られない）
  - [x] `NormalizeVersion` の table-driven テスト

### `goup list` （完了）

- [x] コマンド追加。既定は「直近の stable 版を新しい順で N 件」表示
- [x] 現在バージョンを `*` マーカーで表示、window 外にあれば `  ...` の後に別行で追記
- [x] フラグ:
  - [x] `--all`: pre-release を含める
  - [~] `--pre`: 却下。go.dev の API は stable=true/false の 2 状態のみで、`--all` と実質同義になるため実装しない
  - [x] `-n <N>`: 表示件数上限（既定 10）
- [x] テスト:
  - [x] 既定表示が stable のみ（`TestRenderReleaseList_StableOnlyByDefault`）
  - [x] `--all` で pre-release が含まれる（`TestRenderReleaseList_AllIncludesPreRelease`）
  - [x] 現在バージョンのハイライト
  - [x] `-n` によるトランケート、window 外の current 補足

### `goup version` / `--version` （完了）

- [x] `main.go` に `var version = "dev"` を追加、`-ldflags "-X main.version=vX.Y.Z"` で埋め込み
- [x] `goup version` / `goup --version` / `goup -v` で表示
  > 出力例: `goup v0.2.0 (linux/amd64, go1.26.4)`
- [x] リリースビルドコマンドを `CLAUDE.md` の「リリースビルド」セクションに追記
- [~] 埋め込み無し / タグ付きの出力形状テスト
  > 却下。動作は smoke test（`dev` / `v0.2.0` 両方で確認）で十分、単体テストの ROI が低い

### 副次の整理（着手時に検討）

- [ ] `main.go` の switch 文が肥大化してくるので、サブコマンド dispatch を薄い map か slice ベースに整理するか検討（フレームワーク非導入方針は維持）
  > v0.2.0 完了時点で 7 分岐。まだ許容範囲、v0.3.0 で再検討

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
