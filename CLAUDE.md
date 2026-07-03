# goup

Go 言語本体（toolchain）を更新する CLI。`/usr/local/go` に展開された公式 tarball を安全に入れ替える。

既存の `gup`（github.com/nao1215/gup、`go install` バイナリの一括更新ツール）とは無関係の別ツール。

## 設計方針

- **stdlib-only**: 外部依存パッケージを追加しない。`net/http`, `encoding/json`, `crypto/sha256`, `archive/tar`, `compress/gzip`, `os`, `os/exec`, `flag`, `testing`, `net/http/httptest` のみ使用
- **単一静的バイナリ**: `go build` でそのまま配布可能な単一バイナリにする
- **クロスプラットフォーム**: 対象は WSL2 (Ubuntu) と macOS (Apple Silicon)。Windows ネイティブは非対応（`runtime.GOOS == "windows"` を検出したら明示メッセージを出して終了するのみ）
- **フレームワーク不使用**: サブコマンド dispatch は標準 `flag` パッケージで実装する。Cobra 等は使わない
- **自動 sudo 昇格をしない**: `/usr/local` への書き込みは呼び出し元が `sudo` で実行する前提。権限不足時はエラーメッセージで `sudo` 再実行を促すのみ
- **コメントは英語で書く**

## コマンド

- `goup check`: 現在バージョンと最新安定版を比較表示するのみ（副作用なし）
- `goup update`: ダウンロード → sha256 検証 → バックアップ → 展開 → 起動確認。起動確認に失敗したら自動ロールバック
- `goup rollback`: 直前のバックアップ（`/usr/local/go.bak.<unixtimestamp>`）から手動復元

## バックアップの世代管理

最新 1 世代のみ保持する。新しい `update` を実行する際、既存の `go.bak.*` は新しいバックアップを作る前に削除する。

## テスト容易性

`installer.go` の中核関数は `installRoot`（デフォルト `/usr/local`）と `baseURL`（デフォルト `https://go.dev/dl`）を引数に取る。テストでは `httptest.Server` と `t.TempDir()` を渡すことで、実際の `/usr/local` に触れずに sha256 検証・自動ロールバック・世代管理の挙動を証明する。

## CurrentVersion は VERSION ファイルを直読みする

`CurrentVersion(installRoot)` は `<installRoot>/go/VERSION` の1行目を返す。以前は `go env GOVERSION` を叩いていたが、次の理由でファイル直読みに切り替えた:

- **sudo での PATH 剥奪**: Ubuntu の secure_path は sudo 実行時に `$PATH` を `/usr/sbin:/usr/bin:/sbin:/bin` にリセットするため、`/usr/local/go/bin` にある `go` バイナリが解決できず `sudo goup update` が起動時に失敗していた
- **go.mod toolchain directive との干渉**: カレントディレクトリの `go.mod` に `toolchain` 指定があると `go env GOVERSION` は自動 fetch された別バージョンを返すことがあった（VERSION ファイル読み取りではそもそも関係ない）
- **副次効果**: `go` バイナリが壊れていても（rollback 直前など）installRoot のバージョンが読み取れる

## リリース前チェック

- `go build ./...`
- `go vet ./...`
- `go test ./...`
- `govulncheck ./...`
