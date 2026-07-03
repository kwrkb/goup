# PLAN

goup のロードマップとタスク進捗を記録する。完了タスクは削除せず `[x]` でマークして履歴として残す。

## 現状の把握 (v0.2.0 リリース済み)

- 2026-07-03: **v0.1.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.1.0))
  - サブコマンド: `check` / `update` / `rollback` / `help`
- 2026-07-03: **v0.2.0 リリース済み** ([release](https://github.com/kwrkb/goup/releases/tag/v0.2.0))
  - サブコマンド追加: `install <version> [--pre]` / `list [--all] [-n N]` / `version`
  - `-ldflags "-X main.version=..."` でタグ埋め込み。`go install` 経由だと `dev` になる（設計通り）
  - 実機での自己適用テスト完了（1.26.3 → 1.25.11 → 1.26.4 → rollback）
  - 判明した UX 課題: sudo secure_path で `~/go/bin/goup` が見えず `sudo $(which goup) update` が必要 → v0.3.0 で対応

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

v0.2.0 リリース後の実機動作で `sudo goup update` が `sudo: goup: command not found` で落ちた（Ubuntu の secure_path が `~/go/bin` を PATH から剥奪するため）。ユーザーは `sudo $(which goup) update` と打つ羽目になる。`gup` 経由で go install した非 root ユーザー全員が踏む定石バグ。

CLAUDE.md の設計原則「自動 sudo 昇格をしない」を **TTY 対話時のみ許可** に緩和し、`goup update` をそのまま叩けば sudo プロンプトが自動で出るようにする。

### `runWithElevation` の設計

- [ ] 書き込みコマンド（`update` / `install` / `rollback`）の CLI 層に共通ラッパーを追加。以下の順で判定:
  1. `os.Getuid() == 0`（既に root）→ そのまま実行
  2. `checkWritable(installRoot)` が nil → そのまま実行（ACL 等で権限あり）
  3. `--no-sudo` フラグ or 非 TTY（`os.Stdin.Stat().Mode()&os.ModeCharDevice == 0`）→ 従来通り fast-fail
  4. それ以外 → `syscall.Exec("sudo", os.Args...)` で自己再実行
- [ ] TTY 判定は stdlib のみで実装（`os.Stat` の `ModeCharDevice` ビット参照）。`golang.org/x/term` は依存追加になるため使わない
- [ ] `sudo` 実行は `exec.Command` ではなく `syscall.Exec` を選ぶ。プロセス置換なので子プロセス管理・signal 転送・exit code の受け渡しが自明になる
- [ ] `PATH` 剥奪対策: `syscall.Exec("sudo", ["sudo", os.Args[0], os.Args[1:]...])` に自バイナリの絶対パスを渡す（`os.Executable()` で取得）。sudo 側の secure_path とは無関係にする

### `--no-sudo` フラグ

- [ ] 全書き込みコマンド共通のフラグとして追加
- [ ] 目的: スクリプト・CI で「明示的に昇格を試みない」動作を保証する。TTY 判定だけだと予期しない TTY 検知で暴発する可能性を潰す
- [ ] 実装: `parseInstallArgs` 相当を `update` / `rollback` にも用意（現状フラグ無しなので新規）

### テスト

sudo 環境そのものは単体テストで再現不能なので、判定ロジックだけを分離してテストする:

- [ ] `elevationDecision(uid int, canWrite bool, tty bool, noSudo bool) decision` を純関数として切り出す
- [ ] 4 変数の table-driven テスト（8 パターン + α）で「昇格試行 / そのまま実行 / fast-fail」の 3 分岐を網羅
- [ ] TTY 判定関数 (`isTTY`) は環境依存なので runtime テストは避け、実機 smoke test にリレー
- [ ] 実機テスト: `goup update` を TTY で叩いて sudo プロンプトが出ることを確認、`goup update < /dev/null` で fast-fail に落ちることを確認

### ドキュメント

- [ ] `README.md` の Usage を書き換え。`sudo goup update` を `goup update` に変える（sudo は不要と説明）
- [ ] 「非対話環境では従来通り」の注意書きを追加
- [ ] CLAUDE.md の該当設計方針は既に更新済み

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
