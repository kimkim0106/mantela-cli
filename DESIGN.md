# mantela-cli 設計メモ

状態: 設計中（2026-10-24）。コマンド名は一旦 `mantela-cli`。

## 目的

Mantela で相互接続している交換局の運用者が、**内線・接続先局の定義を1か所で管理**し、そこから

- 公開用の `mantela.json`（Mantela 0.1.x）
- Asterisk（pjsip）の設定断片

を生成できるようにする。手更新による Mantela と PBX 実設定のズレをなくすのが主目的。

### 非目的

- `mantela.json` の公開・PBX への反映（シェルスクリプトや CI に任せる。plugin/hooks 機構は持たない）
- Asterisk 設定全体の生成（各局の手書きダイヤルプランと共存させる）。mantela-cli が持たない設定（transport、global、アプリ本体のコンテキストなど）は、手書きや別のツール（Ansible のテンプレートなど）で用意する
- chan_sip（Asterisk 21 で削除済み）。対象は pjsip のみ
- Asterisk 以外の PBX（ただし Mantela 側は PBX に依存しない形に保つ。後述「PBX 依存の境界」）
- Mantela の `sipUsername`/`sipPassword` 等の非推奨フィールド

## 実装

- 言語は Go。単一バイナリで配布（linux/amd64, linux/arm64, darwin）。PBX 上にも置くだけで動く
- 対象の Asterisk は 20 と 22 の LTS。pjsip のキー名や既定値はバージョンで少しずつ違うので、テストもこの2つで回す（後述「テスト」）
- ライセンスは MIT

### 依存の方針

自前で書くと仕様への追従や正しさの保証が重いもの（TOML のパーサーなど）だけに依存し、数十行で書けるものは標準ライブラリで書く。テストだけで使う依存は、本体のバイナリに入らないので許容する。

本体の依存は次の2つだけにする。

| 依存 | 用途 |
|---|---|
| `BurntSushi/toml` | 局ディレクトリの読み込み。推移的な依存が無い |
| `golang.org/x/term` | `secret set` でのパスワードの非表示入力。Go チームが管理する小さなパッケージ |

それ以外は標準ライブラリで書く。

| 用途 | 書き方 |
|---|---|
| CLI の引数 | 標準の `flag`。サブコマンドは自前で振り分ける |
| 対話の入力（TTY で足りない引数を聞く） | 標準の `bufio`。対話用の TUI ライブラリ（`charmbracelet/huh` など）は推移的な依存が多いので使わない |
| UUID の生成 | `crypto/rand` で v4 を作る |
| 相手局の `mantela.json` の取得 | 標準の `net/http` |
| Mantela スキーマの検証（`check`） | 0.1.x の決まり（必須の項目、type の列挙、identifier の文字種、transferTo が必須になる条件）を Go で直接確かめる |
| Asterisk の conf の読み込み（`import --asterisk`） | 自前で書く。テンプレート・`(+)`・`#include` に対応し、キーの順序を保つ |

テストだけで使う依存:

- JSON Schema の検証ライブラリ。golden テストで出力を公式の Mantela スキーマと照らし合わせ、`check` の手書きの検証とずれていないかを確かめる
- Docker の操作（読み込みテスト・通話テスト）にはライブラリ（testcontainers-go など）を使わず、`os/exec` で `docker` コマンドを呼ぶ

### TOML の読み込み

- `BurntSushi/toml` を使う。CLI はファイル単位で作成・削除するので、コメント保持付きの編集は不要（後述）
  - `[pjsip.*]` は**書いた順序を保つ必要がある**（後述）。`toml.Decode` が返す `MetaData.Keys()` は、キーを TOML に現れた順で返すので、それを使って並べ直す
  - 未知のキーは `MetaData.Undecoded()`（どの型にも対応しなかったキーの一覧）で検出し、`check` で知らせる
  - `pelletier/go-toml/v2` は、struct や map に読むと順序が失われる。順序を取るには互換性の保証が無い `unstable` の API が要るので採らない

## 局ディレクトリ（人間が触る前提）

```
<station>/
├── station.toml          # 自局そのもの（[mantela.aboutMe] = aboutMe、[asterisk] = PBX 全体の設定）
├── defaults.toml         # 各要素ファイルの既定値（[extension] / [provider]）
├── extensions/
│   ├── 6000.toml         # ファイル名 = 内線番号（中に extension は書かない）
│   ├── 6001.toml
│   └── 1234.toml
└── providers/
    ├── station-a.toml    # ファイル名 = 固定の識別名（slug）。prefix は中に書く
    └── station-b.toml
```

- 1ファイルに1要素だけを書くので、CLI の add/rm はファイルの作成・削除だけで済み、既存ファイルを書き換えないのでコメントが消えない。git の差分も要素単位になる
- 内線番号の変更は `git mv`。ファイル名と中身の食い違いが起きない
- 既存ファイルの変更は人間がエディタで行う。メモは正式な `note` フィールドに書く（将来 `set` で書き換えても残る）
- 文字列は必ず引用符（TOML の仕様）。`0123` のような番号が数値化しない（YAML を採らない理由）

### station.toml

```toml
[mantela-cli]
version = 1               # 局ディレクトリの形式の版（後述）

[mantela]
version = "0.1.0"         # 出力する Mantela のバージョン（将来の版を選べるように）

[mantela.aboutMe]         # mantela.json の aboutMe にそのまま出力する
identifier = "123e4567-e89b-12d3-a456-426614174000"   # init で UUID 生成
name = "Example Station"
preferredPrefix = ["200", "2200"]
sipUri = ["sip:pbx.example.net:5060;transport=udp"]

[asterisk]                # Asterisk を使わない局では省略できる
secrets = "/etc/asterisk/pjsip_secrets.conf"   # 生成物の先頭で #include する（後述）
hop = true                # 先頭の 0 で次の局へ渡す PBX 間ホップに対応するか（既定 true。後述「PBX 間ホップ」）
```

- `[mantela]` は `mantela.json` の最上位、`[mantela.aboutMe]` は `aboutMe` に対応する。`[mantela]` 直下には aboutMe 以外の出力設定を置く
- `[mantela.aboutMe]` の中身は道具が解釈せず、そのまま出力する（検証は Mantela 0.1.x の決まりで行い、道具が知らない項目は検証しない）。Mantela に項目が増えても道具を直さずに書ける
- `[mantela-cli] version` は局ディレクトリの形式の版。`[mantela] version`（出力する Mantela の版）とは別物で、テーブル名で区別する

#### 局ディレクトリの形式の版

- 版は `station.toml` だけに書く。局ディレクトリに `station.toml` は1つしかないので、ここで局ディレクトリ全体の形式を表す。要素ファイルや `defaults.toml` には書かない（内線が多いほど手間が増え、版を上げるときに全ファイルを書き換えることになるため）
- 版は整数1つで、道具のバージョン（semver）とは別に数える。上げるのは、古い道具で読むと意味が変わる互換性の無い変更のときだけで、項目を足すだけなら上げない
- `[mantela-cli]` が無い `station.toml` は局ディレクトリではないとみなし、エラーにする（`-C DIR` で別のディレクトリを指定したときに気づけるように）
- 道具が知っている版より新しければ、新しい mantela-cli が必要だとしてエラーにする。古い版を読み替える処理（例: `mantela-cli migrate`）は、版を初めて上げるときに考える

### defaults.toml

各要素ファイルの既定値。`[extension]` の中身は `extensions/*.toml`、`[provider]` の中身は `providers/*.toml` と同じ構造で、各ファイルに書いた値が優先される。

```toml
[extension]
publish = false

[extension.pjsip.endpoint]
disallow = "all"
allow = ["alaw", "ulaw", "opus"]
dtmf_mode = "auto"
language = "ja"
tone_zone = "jp"

[extension.pjsip.aor]
max_contacts = "1"

[provider]
publish = true

[provider.pjsip.endpoint]
disallow = "all"
allow = ["alaw", "ulaw"]
dtmf_mode = "rfc4733"
language = "ja"
tone_zone = "jp"

[provider.trunk]
strip_prefix = true
```

- 内線とトランクでは設定の傾向が違う（`max_contacts` は内線でしか意味がない、dtmf やコーデックも変えることが多い）ので、共通の層は設けない。`language` などを2回書くのは許容する
- `station.toml` と分けておくのは、手でコメントを添えて育てるファイルだから。将来 `station.toml` を CLI で書き換えるようになっても、こちらのコメントは消えない
- ファイルが無ければ既定値なし（各項目の組み込みの既定値を使う）

### 組み込みの既定値

どのファイルにも書かなかったときの値。`defaults.toml` が無い段階（MVP）でも、この値で動く。

| 項目 | 既定値 |
|---|---|
| 内線の `kind` | `"device"` |
| 内線の `type` | `kind` から決める（後述「extensions/<番号>.toml」の表） |
| 内線・provider の `publish` | `true` |
| provider の `enabled` | `true` |
| `[trunk] strip_prefix` | `true` |
| `[trunk] prepend` | `""`（何も前置しない） |
| group の `timeout` | 無し（時間を区切らずに鳴らす） |
| `[asterisk] secrets` | `"/etc/asterisk/pjsip_secrets.conf"` |
| `[asterisk] hop` | `true` |

### 要素ファイルの Mantela 項目（トップレベルと `[mantela]`）

Asterisk 側の `[pjsip.*]`（次節）と同じく、Mantela 側にも素通しの逃げ道を設ける。

- トップレベル: 道具が知っている項目。よく書くもの（`name`, `type` など）はここ
- `[mantela]`: 道具は解釈せず、出力する要素（`extensions[]` / `providers[]` の1件）にそのままマージする。Mantela の新しい項目もここに書ける

```toml
# extensions/6001.toml
name = "黒電話"
type = "dialphone"

[mantela]
image = "https://example.net/kurodenwa.jpg"
geolocationCoordinates = { latitude = 35.68, longitude = 139.76 }
```

トップレベルの項目が Mantela のどこへ出るかは次のとおり。

| 種別 | そのまま出力 | 道具が作って出力 | 出力しない（道具だけが使う） |
|---|---|---|---|
| extension | `name`, `type`, `identifier`, `model` | `extension`（ファイル名）, `transferTo`（`members`）, `unavailable`（後述） | `kind`, `publish`, `note`, `members`, `timeout`, `target`, `[secret]` |
| provider | `name`, `prefix`, `identifier`, `url`（→ `mantela`） | `unavailable`（後述） | `enabled`, `publish`, `note`, `[secret]` |

- `[mantela]` に、トップレベルや道具が作る項目と同じキーを書いたら `check` でエラー（例: `[mantela] name`、`[mantela] transferTo`、`[mantela] unavailable`）。書き方を1通りに保つため
- `[mantela]` の中身は、出力後に Mantela 0.1.x の決まりで検証する。道具が知らない項目は検証しない
- `defaults.toml` でも `[extension.mantela]` / `[provider.mantela]` が使える

### unavailable の決まり方

`unavailable` は内線・provider ともトップレベルに書く。道具が値を決めることがあるため、素通しの `[mantela]` には置かない。上から順に、最初に決まった値を出力する。

1. ファイルに `unavailable` を明示していれば、その値
2. （将来）接続状況から判定した値（後述「将来案」）
3. provider で `enabled = false` なら `true`
4. それ以外は出力しない

### pjsip 設定（`[pjsip.*]`）

Asterisk 側の設定は `[pjsip.<セクション種別>]`（`endpoint` / `aor` / `auth` / `registration` / `identify`）に、**Asterisk のキー名と値のまま**書く。道具は抽象化した独自キーを持たない。Asterisk を使う人は pjsip のキー名を知っているので、別名を覚えさせるより素直なため。

```toml
[pjsip.endpoint]
direct_media = "no"
rtp_symmetric = "yes"
dtmf_mode = "inband"

[pjsip.aor]
max_contacts = "5"
qualify_frequency = "60"
```

`pjsip.conf` はキーの順序に意味がある（`disallow = all` は `allow` より先でなければ、コーデック指定が消える）。そのため、キーは次の規則で出力する。

- 書いた順のまま出力する。アルファベット順などに並べ替えない（出力の決定性は「同じ入力なら同じ順序」で保つ）
- 配列は同じキーを複数行で出力する（`allow = ["alaw", "ulaw"]` → `allow = alaw` / `allow = ulaw`。identify の `match` なども同様）
- `defaults.toml` と要素ファイルの両方に同じキーがある場合、**位置は `defaults.toml` での位置、値は要素ファイルの値**にする。要素ファイルにしか無いキーは後ろに足す。これで `defaults.toml` で `disallow` → `allow` と並べておけば、各ファイルで `allow` だけ上書きしても順序が崩れない
- 1つのセクションの中は、「`type` → 道具が生成する骨組みのキー（`aors`、`auth` など）→ 道具が補う `context` → 利用者のキー（上の規則の順）」の順に出力する

セクションの骨組みに当たるキー（`type`、endpoint の `aors` / `auth` / `outbound_auth`、auth の `username`、registration の `server_uri` / `client_uri`、identify の `endpoint` / `match` など）は道具が生成する。これらを `[pjsip.*]` に書いたら `check` でエラーにする。移行時に変えたいものは `[trunk]` の `username` / `contact_user` のような専用の項目で上書きする。

- `context` を書かなければ、道具が既定の生成コンテキストを入れる（後述「ダイヤルプランの持ち方」）
- 発信者番号（`callerid`）は内線の `name` から作らない。日本語の名前は、ATA や電話機によっては正しく表示されないため。必要なら `[pjsip.endpoint] callerid` に書く
- transport は生成しない。transport は利用者の `pjsip.conf` に定義し、使う名前を `[pjsip.endpoint] transport`（登録は `[pjsip.registration] transport`）で指定する。全内線・全トランクで同じなら `defaults.toml` にまとめて書く
- 生キーの名前や値は検証しない（Asterisk のバージョン差を道具が追いかけない）
- 値は文字列・整数・真偽値を受け付け、そのまま文字列にして出力する（`max_contacts = 5` → `max_contacts = 5`、`rtp_symmetric = true` → `rtp_symmetric = true`）。配列は前述のとおり複数行にする

### extensions/<番号>.toml

```toml
name = "黒電話"
kind = "device"           # device | group | dialplan（省略時は device。後述）
type = "dialphone"        # Mantela の端末種別。省略時は kind から決める
identifier = "A1B2C3D4"   # 省略したら出力しない（add は8桁英数字を採番して書く）
model = "600-A2"
publish = false           # Mantela に載せない
note = "メモ（Mantela にも Asterisk にも反映されない）"

[pjsip.endpoint]
dtmf_mode = "inband"

[pjsip.aor]
max_contacts = "5"
```

```toml
# extensions/6000.toml（全台呼び出し）
name = "全台呼び出し"
kind = "group"
members = ["6001", "6002"]   # 内線番号で書く
timeout = 30
```

```toml
# extensions/1234.toml（エコーテスト）
name = "エコーテスト"
kind = "dialplan"
target = "echo-test,s,1"     # 利用者の手書きコンテキストへ Goto
```

`kind` ごとの意味は次のとおり。省略したら `device` になる。

| kind | Asterisk 側 | Mantela 側 | `type` の既定 |
|---|---|---|---|
| `device` | pjsip endpoint/aor/auth を生成 | そのまま | `phone` |
| `group` | `members` を同時鳴動（`Dial(PJSIP/a&PJSIP/b,timeout)`） | `transferTo` = members の identifier | members 1件なら `alias`、2件以上なら `main` |
| `dialplan` | 利用者の手書きダイヤルプランへ `Goto(target)` | そのまま | `application` |

- `dialplan` により、アプリ本体（エコーテスト等）は各局の `extensions.conf` に残し、道具は番号の割当だけを持つ
- Mantela の `alias`（別の内線への転送）は「members が1件の group」で表す。kind に `alias` を置かないのは、Mantela の type `alias` と意味が衝突するため
- `transferTo` が identifier の配列であることは Mantela 0.1.x のスキーマで確認済み（`type` が alias/main/switchboard のとき必須）
- `identifier` を省略した内線は、Mantela に identifier を出力しない。`build` のたびに採番すると出力が毎回変わってしまうため。ただし group の members に入る内線は `transferTo` に identifier が要るので、無ければ `check` でエラーにする
- `device` は、電話機、FAX、モデムなど、Mantela の `type` を問わず pjsip の endpoint を持つ端末すべてを指す。値の名前を `phone` にしないのは、Mantela の type `phone` と紛らわしいため
- group の members に入れられるのは `kind = "device"` の内線だけ。`dialplan` や別の group の内線を入れると、生成した `Dial(PJSIP/...)` が失敗するため
- `type` が `main` / `switchboard` / `alias` の内線は、Mantela では `transferTo` が必須で、`transferTo` を作るのは group だけなので、`kind = "group"` でなければ `check` でエラーにする（`kind` の書き忘れを知らせる）。逆に、`kind = "group"` で `type` がそれ以外なのは構わない
- Mantela の任意フィールド（`image` / `geolocationCoordinates` など）は `[mantela]` に書く。`unavailable` だけはトップレベル（前述）

### providers/<slug>.toml

```toml
name = "Station A"
prefix = "300"
identifier = "987fcdeb-51a2-43d1-9c4e-5a6b7c8d9e0f"   # add provider で相手の Mantela から取得
url = "https://station-a.example.org/.well-known/mantela.json"   # Mantela の `mantela` として出力
enabled = true
publish = true
# unavailable = true      # 省略時の決まり方は前述

[trunk]                   # 無ければトランクを生成しない（Mantela 掲載のみ）
mode = "register"         # register | accept | ip
host = "192.0.2.10"
strip_prefix = true       # 発信時に prefix を削る（既定 true）
prepend = ""              # 削った後に前置する番号
# 移行時の引き継ぎ用上書き（省略時は slug から生成）
username = "SIP-TRUNK-12345678"
contact_user = "SIP-TRUNK-12345678"
```

`[trunk] mode` ごとの意味は次のとおり。

| mode | 登録 | 生成するもの | パスワード | `host` |
|---|---|---|---|---|
| `register` | こちらから相手へ登録 | endpoint/aor/auth（outbound）/registration/identify | 相手から受け取る（`secret set`） | 必須 |
| `accept` | 相手がこちらへ登録 | endpoint/aor/auth（inbound） | こちらで発行（`secret gen`） | 不要 |
| `ip` | 登録しない（tailnet 等の固定 IP） | endpoint/aor/identify | なし | 必須 |

- `station.toml` の `[mantela.aboutMe]` とは見た目が似ているが、形式は共用しない。aboutMe は自局が自分で決めて名乗る内容で、provider は自局から見た相手局との接続である。provider には、相手が決めた内容の写し（`name`, `identifier`）と、自局の都合（`prefix`, `enabled`, `[trunk]`）が混ざっている。特に `preferredPrefix`（相手が希望する番号の一覧）と `prefix`（自局のダイヤルプランで実際に割り当てた番号）は別物なので、同じ形式にすると区別が見えにくくなる
- 共通点は実装側で活かす。aboutMe の Go の型は `station.toml` の読み込みと相手の `mantela.json` の取得（`add provider` / `import --mantela` / `check --remote`）で共用する
- 相手の `mantela.json` の URL を `mantela` ではなく `url` と書くのは、素通し用の `[mantela]` テーブルと名前がぶつかるため（TOML では同じキーを値とテーブルの両方に使えない）。`add provider` はこの URL を取得に使う
- `accept` は1局につき1認証の場合だけを扱う。共通の認証情報で複数局が登録してくる型は、どの局からの呼かを区別できず prefix で振り分けられないため、当面は対象外
- 相手局がフル番号を受け付ける場合は `strip_prefix = false`。番号の付け替えが必要な場合は `prepend` を使う
- Mantela では provider の `name` / `prefix` / `identifier` / `mantela`（URL）が必須なので、`publish = true` の provider には `identifier` と `url` が必要。無ければ `check` でエラーにする。`publish = false` の provider（トランクだけを作る相手）では省略できる

`enabled` と `publish` の関係は次のとおり。

| `enabled` | `publish` | Asterisk | Mantela |
|---|---|---|---|
| true | true | トランク・発信ルートを生成 | 掲載 |
| true | false | トランク・発信ルートを生成 | 載せない |
| false | true | 生成しない | 掲載し `unavailable = true`（前述の決まり方による） |
| false | false | 生成しない | 載せない |

### PBX 依存の境界

- Mantela 側（`[mantela.aboutMe]`、要素ファイルのトップレベルの Mantela 項目と `[mantela]`、`kind` / `members`）は PBX に依存しない
- Asterisk 固有のものは `[asterisk]`、`[pjsip.*]`、`[trunk]`（`username` / `contact_user` など）、`dialplan` の `target`（Asterisk の `context,exten,priority` 書式）に閉じ込める
- 要素ファイルの `[pjsip.*]` は `[asterisk.pjsip.*]` のような名前空間に入れない。`endpoint` / `aor` などは Asterisk の res_pjsip の言葉そのもので読み違える余地が無く、名前空間に入れると `defaults.toml` の見出しが `[extension.asterisk.pjsip.endpoint]` のように深くなるため
- 他の PBX への対応は予定しない。必要になったら、この境界の Asterisk 側だけを PBX ごとに用意する（要素ファイル側も手直しが要る）。そのときは `[pjsip.*]` を `[asterisk.pjsip.*]` の別名として読む形で移れる

## パスワード

- **パスワードは PBX 上の1ファイルだけに置く**（既定かつ推奨）。この場合、生成物・局ディレクトリには一切入らないので、生成物を commit / PR / CI で扱っても安全。TOML に書く非推奨の方法もある（後述）
- 形式は Asterisk の conf そのもの（既定 `/etc/asterisk/pjsip_secrets.conf`、0640 asterisk）

  ```ini
  [secret-6001](!)
  password = xxxxx

  [secret-trunk-station-a](!)
  password = yyyyy
  ```

- テンプレート名は内線が `secret-<番号>`、トランクが `secret-trunk-<slug>`
- 生成側の auth はテンプレート継承で参照する

  ```ini
  [6001](secret-6001)
  type = auth
  auth_type = userpass
  username = 6001
  ```

- テンプレートは参照より先に定義されている必要があるため、読み込み順は道具が保証する。生成した `pjsip.conf` の先頭で `station.toml` の `[asterisk] secrets` を `#include` する。利用者が自分の `pjsip.conf` に足すのは、生成した `pjsip.conf` を読み込む1行だけで、順序を間違えることはない
- `build` は `secrets.required`（必要なテンプレート名の一覧）も出力。局ディレクトリが無い PBX 上でも `secret check` で不足を検出できる
- 反映の順序が重要で、secrets ファイルやテンプレートが無いと pjsip 設定全体の読込が失敗するため、`secret check` が通ってから `pjsip reload` する
- 発行するパスワード（内線、`accept` トランク）は `secret gen` で生成し一度だけ表示。相手局から受け取るもの（`register` トランク）は `secret set` で非表示入力

### TOML に書く（非推奨）

PBX 上で局ディレクトリを直接編集し、git で管理しない運用では、パスワードだけ別のファイルに書くのは手間になる。そのため、要素ファイルにパスワードを書くことも認める。ただし非推奨とし、上の「生成物を commit / PR / CI で扱っても安全」という性質が崩れる範囲を小さく抑える。

```toml
# extensions/6001.toml
[secret]
password = "xxxxx"
```

- 書く場所は、要素ファイル（内線、`register` / `accept` のトランク）の `[secret] password` だけにする。トップレベルではなく専用のテーブルにして、そこに秘密があることが一目で分かるようにする
- 生成する `pjsip.conf` にはパスワードを入れない。出力先に別のファイル `secrets.conf` を作り、外の secrets ファイルと同じ形式（`[secret-6001](!)` のテンプレート）で書き出す。生成した `pjsip.conf` は、外の secrets ファイルと `secrets.conf` の両方を先頭で `#include` する
- 外の secrets ファイルと TOML の両方に同じテンプレート名があれば、`check` でエラーにする（どちらが効くかを分かりにくくしないため）
- TOML に書いた分は `secrets.required` に含めず、`secret check` の対象からも外す
- TOML にパスワードがある間は、`build` と `check` のたびに、どのファイルにあるかと非推奨であることを stderr に警告する
- `import --asterisk` は、これまでどおり `--secrets` で指定したファイルにだけパスワードを書き出す。非推奨の書き方を道具の側から勧めないため
- 作るのは段階3（後述「段階的な実装」）

## CLI

```
mantela-cli init [DIR]                 # station.toml と defaults.toml（コメント付きの雛形）を作る
mantela-cli add extension [NUM] [--name] [--kind] [--type] [--identifier] [--publish=false] ...
mantela-cli add provider [MANTELA_URL] [--slug] [--prefix] [--mode] [--host] ...
mantela-cli rm  <extension NUM | provider SLUG>
mantela-cli list
mantela-cli check                      # 整合性 + Mantela の決まりの検証
mantela-cli build [--mantela FILE] [--asterisk DIR]
mantela-cli build --check              # 出力が変わるなら非0終了
mantela-cli secret gen|set|check|list ...  # パスワードファイルを扱う（通常 PBX 上）
mantela-cli import [--asterisk PJSIP_CONF] [--mantela URL|FILE] ... DIR   # 初回移行用（後述）

# 段階を決めていない将来案
mantela-cli set <extension NUM | provider SLUG> KEY=VALUE ...
mantela-cli check --remote [SLUG ...]  # 相手局の mantela.json と照合（通信あり）
```

- 各コマンドをどの段階で作るかは、後述「段階的な実装」に書く
- 局ディレクトリはカレントディレクトリ。`-C DIR` で変える（git と同じ書き方。全コマンド共通）
- 引数が足りず端末（TTY）なら対話で補う。非 TTY ならエラー（スクリプトから安全に呼べる）
- `add provider` は相手の `mantela.json` から name / identifier / preferredPrefix を補完
- 出力は決定的（同じ入力なら同じ出力。改行固定）。stdout は結果、stderr は警告

### check の内容

- 内線番号・identifier・prefix の重複、prefix の前方一致の重なり（警告）、内線番号と prefix の衝突（警告）
- Mantela type の列挙外、identifier の文字種（Mantela スキーマ: `^[\x21-\x7E]+$`）
- `group` の members 欠落・存在しない内線や `kind = "device"` 以外の内線の参照・identifier の無い内線の参照、`dialplan` の target 欠落
- members が非公開内線を含むのに group が公開されている
- `type` が `main` / `switchboard` / `alias` なのに `kind = "group"` でない（前述）
- `[trunk] mode` ごとの必須項目（`register` / `ip` の `host` など）
- `hop = true` のとき、`0` で始まる内線番号・prefix（エラー）と `preferredPrefix`（警告）
- 道具が生成するキーを `[pjsip.*]` に書いた、トップレベルと `[mantela]` に同じキーを書いた（前述）
- 外の secrets ファイルと TOML の `[secret]` に同じテンプレート名がある（エラー）、TOML にパスワードがある（警告）（前述「TOML に書く（非推奨）」）
- 公開する provider の `identifier` と `url`（前述）
- 公開する要素の必須フィールドと、素通し分を含む出力全体を、Mantela 0.1.x の決まり（前述「依存の方針」）で検証する。道具が知らない項目は検証しない

### check --remote（相手局との照合。将来案）

provider の `name` / `identifier` は相手の aboutMe の写しなので、時間が経つとずれる。各 provider の `url` から `mantela.json` を取得して照合し、次の場合に警告する。

- `identifier` が一致しない（URL の差し替えミス、相手局の作り直しなど）
- `name` が変わった
- 自局の `prefix` が相手の `preferredPrefix` に含まれていない（意図的な場合もあるので警告に留める）
- 相手の providers に自局（`[mantela.aboutMe] identifier`）が載っていない（相手側が接続を未設定の可能性）
- 取得に失敗した、または取得した内容が Mantela 0.1.x の決まりに合わない

- 通信が発生するので、通常の `check` とは分けて明示的に実行する。`build --check` にも含めない
- `url` の無い provider は対象外。SLUG を指定すればその provider だけを照合する
- 警告だけで局ディレクトリは書き換えない。CI で定期的に回す使い方を想定

## 生成物

```
<asterisk-dir>/               # 例: /etc/asterisk/mantela-cli/
├── pjsip.conf                # 先頭で secrets を #include。endpoint / aor / auth / registration / identify
├── extensions.conf           # 下記コンテキスト
├── secrets.required
└── secrets.conf              # TOML にパスワードを書いたときだけ（非推奨。前述）
```

利用者は自分の `/etc/asterisk/pjsip.conf` / `extensions.conf` に `#include mantela-cli/pjsip.conf` のような行を1行ずつ足すだけ。

#### PBX 側に求めるもの

生成物は、mantela-cli の外で用意される設定を名前で参照する。どの手段（手書き、別のツール）で用意する場合も、次を満たす必要がある。

| 生成物が参照するもの | 局ディレクトリでの書き方 | PBX 側で用意するもの |
|---|---|---|
| secrets のテンプレート | 内線の番号、トランクの slug（`secret-6001`、`secret-trunk-<slug>`） | `[asterisk] secrets` のファイルにある同名のテンプレート |
| transport | `[pjsip.endpoint] transport`、`[pjsip.registration] transport` | `pjsip.conf` にある同名の transport |
| dialplan の Goto 先 | `kind = "dialplan"` の `target` | `extensions.conf` にある同名のコンテキスト |
| 自局の接続先 | aboutMe の `sipUri` | `sipUri` で待ち受ける transport（NAT や外向きの名前があるので、値が一致するとは限らない） |

- mantela-cli が確かめるのは、secrets のテンプレートだけ（`secrets.required` と `secret check`）
- ほかの参照先の確認は将来案（後述「将来案」）

- 生成するファイルの名前は Asterisk の標準の設定ファイル名と同じにする。どの設定ファイルの断片かが名前で分かるため
- 利用者のファイルと同じ名前になるので、出力先は専用のディレクトリにする。生成するファイルの先頭には mantela-cli が生成したことを示す印を入れ、`build` は、印の無い同名のファイルが出力先にあれば上書きを拒否する（`/etc/asterisk` そのものを出力先に指定して、利用者の設定を消す事故を防ぐ）

#### 生成物の印

```ini
; generated by mantela-cli. DO NOT EDIT.
; sha256:3f2a…（この行より後ろの本文のハッシュ）
```

- 印には、生成物の本文（印の行より後ろ）の sha256 を入れる。本文が同じならハッシュも同じなので、出力の決定性と `build --check` を崩さない
- 本文とハッシュが合わなければ、生成物が手で編集されたとみなす。`build` は警告して上書きを止め、`secret check` も反映前に同じ警告を出す
- 道具のバージョンは印に入れない。入れると道具を更新しただけで出力が変わり、`build --check` が差分ありと判定してしまうため
- git のコミットハッシュも入れない。git が無い PBX では取れず、生成物を同じリポジトリにコミットするとコミットのたびに変わってしまうため
- `mantela.json` には印を入れない。JSON にはコメントが書けず、印のために Mantela に無い項目を足すのも避けたいので、上書きの保護は Asterisk 用の生成物だけにする

#### 出力の並び順

同じ入力から同じ出力を作るため、並び順を決めておく。

- `mantela.json` の `extensions` は内線番号の文字列順、`providers` は slug の文字列順に並べる。数値順にしないのは、`*98` のような数字以外を含む番号も扱えるようにするため（`999` より `1000` が前に来る）
- `mantela.json` の各要素の中は、Mantela のスキーマに書かれた項目の順に出し、素通しの `[mantela]` の項目はその後ろに書いた順で足す
- 生成する `pjsip.conf` は、内線（内線番号の文字列順）、トランク（slug の文字列順）の順に並べる。内線・トランク1つの中は auth → aor → endpoint → registration → identify の順にする（生成しないセクションは飛ばす）
- 生成する `extensions.conf` の各コンテキストの中も、内線番号・slug の文字列順に並べる

#### mantela.json の `$schema`

`mantela.json` の先頭に、`[mantela] version` に対応する公式スキーマの URL を `$schema` として入れる（0.1.x なら `https://tkytel.github.io/mantela/0.1.x/mantela.schema.json`）。Mantela のスキーマは未知の項目を禁じていないので、入れても決まりに反しない。エディタや他の道具で検証が効くようになる

### ダイヤルプランの持ち方

生成するコンテキストは利用者のダイヤルプランから `include =>` される部品に徹する。

| コンテキスト | 中身 |
|---|---|
| `mantela-internal` | 内線・group・dialplan |
| `mantela-outbound` | `enabled` な provider ごとに `_<prefix>X.` → `strip_prefix` / `prepend` を適用して `Dial(PJSIP/<番号>@<trunk>)` |
| `mantela-hop` | `hop = true`（既定）のとき生成。`_0X.` → 先頭の `0` を外して `mantela-outbound` へ渡す（後述「PBX 間ホップ」） |
| `mantela-phones` | `include => mantela-internal` + `include => mantela-outbound`（`hop = true` なら `mantela-hop` も）。内線 endpoint の既定 context |
| `mantela-from-<slug>` | `include => mantela-internal`（`hop = true` なら `mantela-hop` も）。トランク endpoint の既定 context |

- 単純な局: 既定の context のまま使えば、`#include` 2行だけで動く
- 独自の発信規則（外線・特番等）を持つ局: `defaults.toml` の `[extension.pjsip.endpoint] context = "from-phones"` などで利用者が所有するコンテキストを指定し、そこで部品を include する

  ```ini
  ; extensions.conf（利用者が書く）
  [from-phones]
  include => mantela-internal
  include => mantela-outbound
  exten => _9X.,1,Dial(PJSIP/${EXTEN:1}@carrier)   ; 局独自の規則（例: 9 で外線）
  ```

- トランクからの着信も同様に、`providers/<slug>.toml` の `[pjsip.endpoint] context`（全トランク共通なら `defaults.toml` の `[provider.pjsip.endpoint] context`）で利用者のコンテキストへ向けられる
- `hop = false` のとき、他局からの着信は自局の内線にだけ届く。他局への中継は、次の PBX 間ホップだけで行う（prefix を見て自動で中継する仕組みは持たない）

### PBX 間ホップ

直接つながっていない局を、つながっている局を経由して呼ぶための慣習に合わせる（参考: [MikoPBX を Proxmox 上に構築して他の PBX と相互接続する記事の「PBX 間ホップ」](https://zenn.dev/kusaremkn/articles/abd760f9f2f450#pbx-%E9%96%93%E3%83%9B%E3%83%83%E3%83%97)）。

- 番号の先頭の `0` が「次の局へ渡す」目印になる。各局は `_0X.` を受けて先頭の `0` を外し、残りを自局の発信ルートで解釈し直す
- 例: 自局が A 局（prefix `510`）とつながり、B 局（A 局から見た prefix `617`）とはつながっていないとき、`0 510 0 617 1005` と発信すると、自局 → A 局 → B 局の内線 `1005` の順に届く
- 経路は発信者が番号の中に書く。局を通るたびに番号が `0` と prefix の分だけ短くなるので、呼が局の間を回り続けることはない
- 同じ局を何度も通る経路も制限しない（ループ対策は持たない）。局間ホップの試験や、遠距離の通信を再現するために、意図的に遠回りの経路で発信することがあるため
- 既定（`hop = true`）では、`mantela-hop` を生成し、`mantela-phones` と `mantela-from-<slug>` から include する。`hop = false` にすると生成しない
- 先頭が `0` の番号はホップ用に予約されるので、`hop = true` のときは `0` で始まる内線番号と provider の prefix を `check` でエラーにする。自局の `preferredPrefix` が `0` で始まる場合は、相手局のホップと衝突しうるので警告する

## 反映の例（道具の外）

```sh
mantela-cli build --asterisk out/ --mantela mantela.json
rsync -a out/ pbx:/etc/asterisk/mantela-cli/
ssh pbx 'mantela-cli secret check /etc/asterisk/mantela-cli/secrets.required && asterisk -rx "pjsip reload" && asterisk -rx "dialplan reload"'
cp mantela.json ../site/public/.well-known/ && git -C ../site commit -am "update mantela" && git -C ../site push
```

PBX 上で編集する運用では、同じ手順を PBX 上で完結させる。

## import（初回移行用）

既存の Asterisk の `pjsip.conf` と、公開中の `mantela.json` から局ディレクトリを作る。2つは足りない情報を補い合うので、1回のコマンドで両方を読み、照合してから書き出す。

```
mantela-cli import [--asterisk PJSIP_CONF] [--mantela URL|FILE] [--secrets FILE] [--map ENDPOINT=SLUG ...] DIR
```

| 情報 | 取り込み元 |
|---|---|
| 内線の一覧、pjsip の設定、パスワード | `--asterisk`（`pjsip.conf`） |
| トランクと `mode`（`registration` があれば `register`、`identify` があれば `ip`、こちらで認証して登録を受けていれば `accept`） | `--asterisk` |
| 自局の aboutMe | `--mantela` |
| 内線の name / type / identifier / model、group（`transferTo` → `members`） | `--mantela` |
| provider の name / identifier / url / prefix（公開している providers の prefix が、そのまま自局のルーティング） | `--mantela` |

- **ダイヤルプラン（`extensions.conf`）は読まない**。自由に書けるので、prefix とトランクの対応やグループを確実には推測できないため。ルーティングの情報は `mantela.json` の providers から得る
- MikoPBX など、Asterisk を内部で使う PBX からも移行できる。それらが生成した `/etc/asterisk/pjsip.conf` を読めばよく、製品ごとの処理（DB の読み込みなど）は持たない
- 書き出し先 DIR は空でなければ拒否する（既存の局ディレクトリへのマージはしない）。どちらか片方だけの指定でもよい

#### 照合

- 内線: endpoint 名と Mantela の内線番号で照合する。endpoint 名が番号でなければ、TTY なら対話で聞き、非 TTY なら警告して飛ばす
- トランクと provider: 自動では結びつけない。TTY なら対話で選び、非 TTY なら `--map ENDPOINT=SLUG` で指定する。結びつかなかったものは別々のファイルとして書き出し、`check` のエラーで知らせる
- Mantela にあって endpoint が無い内線は、type が `application` 等なら `kind = "dialplan"`（`target` は空のまま。`check` で知らせる）、それ以外は `kind = "device"` として警告する
- endpoint があって Mantela に無い内線は `kind = "device"`, `publish = false`

#### pjsip.conf の扱い

- `;` のコメント、テンプレート `(!)` とその継承、`(+)`、`#include` を解決してから読む
- 道具が生成するキー（前述）は取り込み時に消費し、残りのキーは書いた順のまま `[pjsip.*]` に入れる。設定が移行で失われない
- 全内線（または全トランク）で同じ値のキーは `defaults.toml` の `[extension.pjsip.*]` / `[provider.pjsip.*]` にまとめる
- トランクの認証ユーザー名・contact_user は `[trunk]` の `username` / `contact_user` として保持する（相手局の設定を変えさせないため）
- **パスワードは `--secrets` で指定したファイルに secrets 形式で書き出す**。局ディレクトリの中には決して書かない。パスワードが見つかったのに `--secrets` が無ければエラーにする
- transport やグローバル設定は取り込まない（利用者の `pjsip.conf` に残す）

#### 雛形の mantela.json を読む

`--mantela` には、公開中の `mantela.json` だけでなく、ほかの道具が作った雛形も渡せるようにする。たとえば [mikopbx-mantela](https://github.com/scrwnl/mikopbx-mantela) は、MikoPBX の設定 DB から `mantela.json` の雛形を作る。そうした雛形は、人が後で埋める前提で空の値を含み、Mantela 0.1.x のスキーマに合わないことがある。そのため `--mantela` の入力はスキーマで検証せず、次の規則で緩めに読む。書き出した局ディレクトリは、最後の `check` で検証する。

- 空の文字列（`""`）は「値が無い」として扱う。identifier が空なら、`init` / `add` と同じ規則で採番する（自局は UUID、内線は8桁英数字）。provider の identifier と url は空のまま残し、`check` で知らせる
- `version` が 0.1.x でない（`0.0.0` や欠落）ときは、警告して読み進める
- 綴りや形の違う既知の項目は直して読み、警告する。たとえば aboutMe の `prefferedPrefix`（f が2つ）は `preferredPrefix` として読み、文字列なら1要素の配列にする
- 非推奨の項目（`sipUser` / `sipPassword` / `sipServer` / `sipPort` など）は取り込まず、警告する。パスワードを局ディレクトリに書かないため
- `$schema` は取り込まない。上のどれにも当たらない未知の項目は、素通し用の `[mantela.aboutMe]` / `[mantela]` に入れる
- 内線番号はファイル名になるので、内線番号として使えない文字を含む `extension` はエラーにする。`extension` が内線番号らしくなく `name` が数字だけのときは、2つが入れ替わっている可能性を警告する（TTY なら入れ替えるか聞く）
- 内線の type はそのまま取り込む。雛形ではすべて `phone` になっていることが多いので、取り込み後の一覧で見直しを促す

取り込み後は自動で `check` を実行し、手で直すべき箇所（`dialplan` の `target`、結びつかなかったトランクなど）を一覧にする。

## テスト

生成した設定が正しいかを、3つの層で確かめる。どの層をどの段階で作るかは、後述「段階的な実装」に書く。

| 層 | 中身 | Asterisk | 作る段階 |
|---|---|---|---|
| 1. golden テスト | `testdata/` の局ディレクトリの例から `build` した出力を、期待ファイルと比べる。キーの順序、配列の展開、`defaults.toml` との合成、`check` のエラーと警告もここで確かめる。`mantela.json` は公式の Mantela スキーマとも照らし合わせる | 不要 | 段階0 |
| 2. 読み込みテスト | 生成物を Asterisk 1台に読み込ませ、起動ログに設定のエラーが無いことと、endpoint・aor・コンテキストが期待どおりにあることを確かめる。secrets ファイルやテンプレートが足りないときに読み込みが失敗し、`secret check` がそれを事前に見つけられることも確かめる | 1台 | 段階1 |
| 3. 通話テスト | 2〜3台を mantela-cli の設定で接続し、`[trunk] mode` ごとの登録と、相手局への着信を確かめる。着信先は `kind = "dialplan"` で印のファイルを作るコンテキストにし、音声を使わずに判定する。内線の登録は、別の Asterisk を電話機の代わりにして試す | 2〜3台 | 段階4 |

- 1層目は `go test` で毎回、2層目と3層目は `go test -tags=integration` で PR ごとか夜間に回す
- Asterisk の公式の Docker イメージは無いので、`test/asterisk/Dockerfile` で公式のソース tarball からビルドする。取得したファイルは署名かハッシュで検証し、`menuselect` でテストに要るモジュールだけを有効にする
- バージョンは 20 と 22 の LTS のマトリクスで回す。ビルドに時間がかかるので、CI ではイメージをキャッシュし、Dockerfile が変わったときだけ作り直す
- Docker の操作はライブラリを使わず、`os/exec` で `docker` コマンドを呼ぶ（前述「依存の方針」）

## 段階的な実装

最低限の MVP から始め、段階ごとに機能を足す。各段階の終わりで、その時点の機能だけで実際に運用できる状態にする。

### 段階0: MVP（手で書いた局ディレクトリから生成するだけ）

自局の `mantela.json` と Asterisk の設定を1か所から作り、ズレをなくす。それ以外は後回しにする。

- コマンドは `build` だけ。`mantela.json` と、Asterisk 用の `pjsip.conf`・`extensions.conf`（専用のディレクトリに出す）を出す
- 生成物の印（本文の sha256）を入れ、印の無い同名のファイルと、手で編集された生成物は上書きしない（利用者の設定を消さないため、MVP から入れる）
- `station.toml` の `[mantela-cli] version` を読み、無ければエラーにする
- 局ディレクトリは手で書く（`init` / `add` は無い）。対応するのは `station.toml`、`extensions/`、`providers/` のみで、`defaults.toml` は無い
- 内線は `kind = "device"` だけ
- `hop` は段階2で PBX 間ホップを作るまで無視する（`mantela-hop` を生成しない）
- トランクは `mode = "register"`（自局から相手局へ登録しに行く方式）だけ。自局でいま使っている方式に合わせた
- `[pjsip.*]` の素通しと、キーの順序を保つ処理は入れる。設計の中でいちばん技術的に不確かな部分なので、早いうちに確かめる
- パスワードは外の secrets ファイルに手で書く（内線は `secret-<番号>`、トランクは `secret-trunk-<slug>`）。生成物はテンプレート継承で参照し、先頭で `#include` するところまで
- 検査は、重複と Mantela の必須項目だけを `build` の中で確かめ、エラーなら止める
- golden テスト（1層目）はこの段階から作る

### 段階1: 安心して運用に乗せる

- `check` を独立したコマンドにし、`build --check` を加える
- `secrets.required` の出力と `secret check`（反映前に不足を検出する）
- `defaults.toml`
- 読み込みテスト（2層目。Asterisk を自前でビルドしたイメージ）

Asterisk に読み込めない設定を反映してしまう事故がいちばん痛いので、`check` と secrets の確認を先に入れる。

### 段階2: 表現できるものを増やす

- `kind = "group"` / `"dialplan"`
- 残りのトランクの `mode`（`accept` / `ip`）
- 要素ファイルの素通し用 `[mantela]` と、`unavailable` の決まり方
- ダイヤルプランの部品（`mantela-phones` など）を、利用者のコンテキストから include する使い方
- PBX 間ホップ（`hop`）

### 段階3: 手で書く手間を減らす

- `init` / `add` / `rm` / `list`（TTY なら対話で補う）
- `secret gen` / `secret set` / `secret list`
- TOML にパスワードを書く非推奨の方法（`[secret] password`。前述「TOML に書く（非推奨）」）
- `add provider` で、相手の `mantela.json` から項目を補う

### 段階4: 他の局が使い始められるようにする

- `import --mantela`（依存が増えないので先に作る）、続いて `import --asterisk`
- 通話テスト（3層目）

import は主に他の局が使い始めるときの入口なので最後に回す。自局の移行は、段階0で局ディレクトリを手で書けば済む。

### 段階を決めていない将来案

- `check --remote`（あると便利だが、なくても運用は回る）
- `set`（ファイルは人間が直接編集する前提なので、なくても困らない）
- 接続状況から決める `unavailable`（後述「将来案」）
- エディタでの補完と検証のための JSON Schema（後述「将来案」。早くても段階3で `init` / `add` を作るとき）
- PBX 側の参照先の確認（後述「将来案」）
- サーバ側での手作業の調整（後述「将来案」）

## 将来案

### 接続状況を unavailable に反映する

Mantela の `unavailable` は「（一時的に）利用できないとき true」なので、Asterisk の接続状況を反映する考え方とは合っている。状態は次のように取れる。

| 対象 | 判定の材料 | 取り方 |
|---|---|---|
| トランク `register` | 自局からの登録の成否 ＋ OPTIONS への応答 | `pjsip show registrations` / AMI `PJSIPShowRegistrationsOutbound` |
| トランク `accept` | 相手が登録しているか ＋ OPTIONS への応答 | `pjsip show contacts` / AMI `PJSIPShowContacts` |
| トランク `ip` | OPTIONS への応答（aor に静的な contact と `qualify_frequency` が要る） | 同上 |
| 内線 | 電話機が登録しているか | 同上 |

- `asterisk -rx` の出力は Asterisk のバージョンで形式が変わるので、読み取る処理がおかしくなりやすい。AMI は構造化されたデータが取れるが、`manager.conf` に認証情報が要る
- **`build` には混ぜない**。`build` は局ディレクトリだけから決定的に作る性質（`build --check` や CI での差分確認）を保つ。別のコマンド（例: PBX 上で cron から `mantela-cli status apply mantela.json`）が、出来上がった `mantela.json` の `unavailable` だけを書き換える形にする
- 一瞬の切断でばたつかないよう、「N 回続けて失敗したら true」のようなしきい値を設ける
- 状態が変わるたびに公開し直すことになるので、いまの「サイトのリポジトリに commit / push して公開」という経路とは相性が悪い。PBX から直接配信するなど、公開経路の見直しとセットで検討する

### PBX 側の参照先の確認

- 生成物が PBX 側に求めるもの（前述「PBX 側に求めるもの」）を、`secrets.required` を広げた一覧として書き出す（secrets のテンプレート名、transport 名、dialplan の Goto 先のコンテキスト）
- `secret check` を広げたコマンドで、PBX の `/etc/asterisk` の設定を読み、一覧の参照先が全部あるかを反映前に確かめる。Asterisk の conf を読む処理は `import --asterisk` 用のものを使い回せる
- `sipUri` のポートと transport は、PBX の transport の `bind` / `protocol` と比べ、合わなければ警告に留める（食い違いが正しい場合もあるため）

### サーバ側での手作業の調整

当面は考えない。まずは内線と provider の生成だけで手間を減らす。生成物を手で直した場合は、これまでどおり印のハッシュで見つけて `build` が上書きを止める。必要になったら次を検討する。

- 生成物を直さずに、利用者の `pjsip.conf` で `[6001](+type=endpoint)` のように既存のセクションへ項目を足す（Asterisk の `(+)` と型での絞り込み）。include をまたぐと効かない場合があるという報告があるので、Asterisk 20 と 22 で効くかを読み込みテストで確かめてから案内する
- `build` が上書きを止めたときに、調整を局ディレクトリの `[pjsip.*]` へ移すよう案内する

### エディタでの補完と検証（JSON Schema）

- 局ディレクトリの各ファイル（`station.toml`、`defaults.toml`、内線、provider）用の JSON Schema を公開すれば、TOML 対応のエディタ（Taplo を使う VS Code の拡張など）で補完や入力ミスの検出が効く
- ファイルの先頭に `#:schema <URL>` というコメントを書いておくと、エディタがそのスキーマを使う。`init` / `add` が作るファイルにこの行を入れれば、利用者は何もしなくても使える
- スキーマを Go の実装とは別に保守する手間がかかるので、早くても段階3で `init` / `add` を作るときに考える

## 未決事項

決める時期を括弧に書く。

- [ ] PBX 間ホップで、次の局へ渡す前に参考記事のように `Answer()` で一度応答するか（段階2でホップを作るとき、ほかの局と実際につないで決める）
- [ ] 発信ルートで、prefix 単位の `strip_prefix` / `prepend` で表せない規則をどこまで扱うか（必要な局が出てきてから）
- [ ] Mantela の将来バージョン（0.2.x 等）への追従方法。項目の追加は `[mantela.aboutMe]` と要素ファイルの `[mantela]` の素通しで吸収できる。構造が変わる場合に `[mantela] version` で出力を切り替えるか（0.2.x が出てから）
