# PLAN

goup のロードマップとタスク進捗を記録する。完了タスクは削除せず `[x]` でマークして履歴として残す。

## 現状の把握 (v0.3.0 リリース済み)

- 2026-07-03: **v0.1.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.1.0))
  - サブコマンド: `check` / `update` / `rollback` / `help`
- 2026-07-03: **v0.2.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.2.0))
  - サブコマンド追加: `install <version> [--pre]` / `list [--all] [-n N]` / `version`
  - `-ldflags "-X main.version=..."` でタグ埋め込み。`go install` 経由だと `dev` になる（設計通り）
  - 実機での自己適用テスト完了（1.26.3 → 1.25.11 → 1.26.4 → rollback）
  - 判明した UX 課題: sudo secure_path で `~/go/bin/goup` が見えず `sudo $(which goup) update` が必要 → v0.3.0 で対応
- 2026-07-04: **v0.3.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.3.0))
  - 対話 TTY での自動 sudo 昇格 (`syscall.Exec` + `os.Executable()`)。`--no-sudo` フラグ追加
  - TTY 判定はハイブリッド (`/dev/tty` open AND stdin ModeCharDevice) で CI/cron/pipe/redirect を fast-fail
  - `goup update` no-op 時は昇格スキップ (PR #3 codex レビュー対応、v0.2.0 挙動と互換)
  - `--help` を 7 原則で再設計。`goup help <cmd>` / `goup <cmd> --help` で per-command help 提供
  - Linux amd64 / macOS arm64 の prebuilt binary を GitHub Release に添付

## v0.2.0: 指定バージョンインストール + list（完了）

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

## v0.3.0: 対話 TTY での自動 sudo 昇格

### 動機

v0.2.0 リリース後の実機動作で `sudo goup update` が `sudo: goup: command not found` で落ちた（Ubuntu の secure_path が `~/go/bin` を PATH から剥奪するため）。ユーザーは `sudo $(which goup) update` と打つ羽目になる。`go install` で `~/go/bin` に入れた非 root ユーザー全員が踏む定石バグ。

CLAUDE.md の設計原則「自動 sudo 昇格をしない」を **TTY 対話時のみ許可** に緩和し、`goup update` をそのまま叩けば sudo プロンプトが自動で出るようにする。

### `runWithElevation` の設計

- [x] 書き込みコマンド（`update` / `install` / `rollback`）の CLI 層に共通ラッパーを追加。以下の順で判定:
  1. `os.Getuid() == 0`（既に root）→ そのまま実行
  2. `checkWritable(installRoot)` が nil → そのまま実行（ACL 等で権限あり）
  3. `--no-sudo` フラグ or 非 TTY（`os.Stdin.Stat().Mode()&os.ModeCharDevice == 0`）→ 従来通り fast-fail
  4. それ以外 → `syscall.Exec("sudo", os.Args...)` で自己再実行
  > `elevate.go` の `maybeElevate` / `elevationDecision` として実装
- [x] TTY 判定は stdlib のみで実装
  > `/dev/tty` open 可能 AND stdin が character device のハイブリッド。CI / cron / detached は前者で、pipe / regular-file redirect は後者で fast-fail。既知の穴 `< /dev/null` のみ受容。詳細 `implementation-notes.md`
- [x] `sudo` 実行は `syscall.Exec` を選択。プロセス置換で signal / exit code / stdio を sudo に委譲
- [x] `PATH` 剥奪対策: `os.Executable()` で得た絶対パスを argv に載せる

### `--no-sudo` フラグ

- [x] 全書き込みコマンド共通のフラグとして追加
- [x] `parseWriteFlags` を新設して `update` / `rollback` に付与。`parseInstallArgs` は 4 戻り値化して `install` に付与

### テスト

- [x] `elevationDecision(uid, canWrite, tty, noSudo) decision` を純関数として切り出し
- [x] 11 パターンの table-driven テスト (`elevate_test.go`) で run / elevate / fail 3 分岐を網羅
- [x] `isTTY` の runtime テストは avoid、実機 smoke test にリレー
- [x] 実機テスト (2026-07-04, WSL2 Ubuntu):
  - 対話 TTY で `install 1.25.11` → 自動昇格 → sudo プロンプト → 1.26.4 → 1.25.11 成功
  - `echo | update` (pipe) → fast-fail
  - `update --no-sudo` → fast-fail
  - `rollback` → 自動昇格 → 1.26.4 復元成功

### ドキュメント

- [x] `README.md` の Usage を書き換え（`sudo goup update` → `goup update`、非対話環境の挙動を説明）
- [x] Requirements と Out of scope からも sudo 前提の記述を除去
- [x] CLAUDE.md の該当設計方針は更新済み

### 実装メモ

- 昇格判定は install の場合でも `FetchAllReleases` より前で実行する（CLI 層に集約するため）。副作用: `goup install <typo>` / `install <current>` でも sudo プロンプトが先に出る。詳細は `implementation-notes.md` 参照
- `usage()` の "requires sudo" 表記も更新（自動昇格することを明記）
- `runCheck()` の hint も ``Run `sudo goup update` `` → ``Run `goup update` `` に変更

### スコープ外（意図的に含めない）

- **goup 自身での password 収集**: `sudo` に完全に委譲する。ターミナル制御・エコー抑止・タイムアウト等を自力で書かない
- **`--sudo` フラグ**: 明示 opt-in は「デフォルトの逆」で複雑化する。TTY 自動判定 + `--no-sudo` opt-out の 2 択で十分
- **root 権限が要らない環境向けの特別対応** (`chown` された `/usr/local/go` 等): edge case すぎる。既存の `checkWritable` が nil を返すので自動的に非 sudo 経路に流れる

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
