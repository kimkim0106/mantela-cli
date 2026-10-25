# mantela-cli 設計メモ

状態: 設計中（2026-10-24）。コマンド名は仮に `mantela`。

## 目的

mantela で相互接続している交換局の運用者が、**内線・接続先局の定義を1か所で管理**し、そこから

- 公開用の `mantela.json`（mantela 0.1.x）
- Asterisk（pjsip）の設定断片

を生成できるようにする。手更新による mantela と PBX 実設定のズレをなくすのが主目的。

### 非目的

- mantela.json の公開・PBX への反映（シェルスクリプトや CI に任せる。plugin/hooks 機構は持たない）
- Asterisk 設定全体の生成（各局の手書きダイヤルプランと共存させる）
- chan_sip（Asterisk 21 で削除済み）。対象は pjsip のみ
- Asterisk 以外の PBX（ただし mantela 側は PBX に依存しない形に保つ。後述「PBX 依存の境界」）
- mantela の `sipUsername`/`sipPassword` 等の非推奨フィールド

## 実装

- 言語は Go。単一バイナリで配布（linux/amd64, linux/arm64, darwin）。PBX 上にも置くだけで動く
- TOML: `pelletier/go-toml/v2` 想定。CLI はファイル単位で作成・削除するので、コメント保持付きの編集は不要（後述）
  - ただし `[pjsip.*]` は**書いた順序を保つ必要がある**ため、map への Unmarshal ではなくパーサー API で文書の順序ごと読む（後述）
- Asterisk の conf の読み込み（`import --asterisk` 用）は自前で書く。テンプレート・`(+)`・`#include` に対応し、キーの順序を保つ。新しい依存は増やさない
- 対話: `charmbracelet/huh` 想定。CLI: 標準 `flag` か `cobra`（未定）

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
[mantela]
version = "0.1.0"         # 出力する mantela のバージョン（将来の版を選べるように）

[mantela.aboutMe]         # mantela.json の aboutMe にそのまま出力する
identifier = "123e4567-e89b-12d3-a456-426614174000"   # init で UUID 生成
name = "Example Station"
preferredPrefix = ["200", "0200"]
sipUri = ["sip:pbx.example.net:5060;transport=udp"]

[asterisk]                # Asterisk を使わない局では省略できる
secrets = "/etc/asterisk/pjsip_secrets.conf"   # 生成物の先頭で #include する（後述）
# transit = false         # 他局からの着信を別の局へ中継するか（v1 では未実装。常に false）
```

- `[mantela]` は mantela.json の最上位、`[mantela.aboutMe]` は `aboutMe` に対応する。`[mantela]` 直下には aboutMe 以外の出力設定を置く
- `[mantela.aboutMe]` の中身は道具が解釈せず、そのまま出力する（検証は mantela スキーマのみ）。mantela に項目が増えても道具を直さずに書ける

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
- station.toml と分けておくのは、手でコメントを添えて育てるファイルだから。将来 station.toml を CLI で書き換えるようになっても、こちらのコメントは消えない
- ファイルが無ければ既定値なし（各項目の組み込みの既定値を使う）

### 要素ファイルの mantela 項目（トップレベルと `[mantela]`）

Asterisk 側の `[pjsip.*]`（次節）と同じく、mantela 側にも素通しの逃げ道を設ける。

- トップレベル: 道具が知っている項目。よく書くもの（`name`, `type` など）はここ
- `[mantela]`: 道具は解釈せず、出力する要素（`extensions[]` / `providers[]` の1件）にそのままマージする。mantela の新しい項目もここに書ける

```toml
# extensions/6001.toml
name = "事務所 1"
type = "phone"

[mantela]
image = "https://example.net/phone.jpg"
geolocationCoordinates = { latitude = 35.68, longitude = 139.76 }
```

トップレベルの項目が mantela のどこへ出るかは次のとおり。

| 種別 | そのまま出力 | 道具が作って出力 | 出力しない（道具だけが使う） |
|---|---|---|---|
| extension | `name`, `type`, `identifier`, `model` | `extension`（ファイル名）, `transferTo`（`members`）, `unavailable`（後述） | `kind`, `publish`, `note`, `members`, `timeout`, `target` |
| provider | `name`, `prefix`, `identifier`, `url`（→ `mantela`） | `unavailable`（後述） | `enabled`, `publish`, `note` |

- `[mantela]` に、トップレベルや道具が作る項目と同じキーを書いたら `check` でエラー（例: `[mantela] name`、`[mantela] transferTo`、`[mantela] unavailable`）。書き方を1通りに保つため
- `[mantela]` の中身の検証は出力後の mantela スキーマ検証だけ
- defaults.toml でも `[extension.mantela]` / `[provider.mantela]` が使える

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

pjsip.conf はキーの順序に意味がある（`disallow = all` は `allow` より先でなければ、コーデック指定が消える）。そのため、キーは次の規則で出力する。

- 書いた順のまま出力する。アルファベット順などに並べ替えない（出力の決定性は「同じ入力なら同じ順序」で保つ）
- 配列は同じキーを複数行で出力する（`allow = ["alaw", "ulaw"]` → `allow = alaw` / `allow = ulaw`。identify の `match` なども同様）
- defaults.toml と要素ファイルの両方に同じキーがある場合、**位置は defaults.toml での位置、値は要素ファイルの値**にする。要素ファイルにしか無いキーは後ろに足す。これで defaults.toml で `disallow` → `allow` と並べておけば、各ファイルで `allow` だけ上書きしても順序が崩れない

セクションの骨組みに当たるキー（`type`、endpoint の `aors` / `auth` / `outbound_auth`、auth の `username`、registration の `server_uri` / `client_uri`、identify の `endpoint` / `match` など）は道具が生成する。これらを `[pjsip.*]` に書いたら `check` でエラーにする。移行時に変えたいものは `[trunk]` の `username` / `contact_user` のような専用の項目で上書きする。

- `context` を書かなければ、道具が既定の生成コンテキストを入れる（後述「ダイヤルプランの持ち方」）
- 生キーの名前や値は検証しない（Asterisk のバージョン差を道具が追いかけない）

### extensions/<番号>.toml

```toml
name = "事務所 1"
kind = "phone"            # phone | group | dialplan（後述）
type = "phone"            # mantela の端末種別。省略時は kind から決める
identifier = "A1B2C3D4"   # 省略時 add で8桁英数字を採番
model = "Example IP Phone"
publish = false           # mantela に載せない
note = "予備機と入れ替え予定"

[pjsip.endpoint]
dtmf_mode = "inband"

[pjsip.aor]
max_contacts = "5"
```

```toml
# extensions/6000.toml（代表）
name = "代表"
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

`kind` ごとの意味は次のとおり。

| kind | Asterisk 側 | mantela 側 | `type` の既定 |
|---|---|---|---|
| `phone` | pjsip endpoint/aor/auth を生成 | そのまま | `phone` |
| `group` | `members` を同時鳴動（`Dial(PJSIP/a&PJSIP/b,timeout)`） | `transferTo` = members の identifier | members 1件なら `alias`、2件以上なら `main` |
| `dialplan` | 利用者の手書きダイヤルプランへ `Goto(target)` | そのまま | `application` |

- `dialplan` により、アプリ本体（エコーテスト等）は各局の extensions.conf に残し、道具は番号の割当だけを持つ
- mantela の `alias`（別の内線への転送）は「members が1件の group」で表す。kind に `alias` を置かないのは、mantela の type `alias` と意味が衝突するため
- `transferTo` が identifier の配列であることは mantela 0.1.x のスキーマで確認済み（`type` が alias/main/switchboard のとき必須）
- mantela の任意フィールド（`image` / `geolocationCoordinates` など）は `[mantela]` に書く。`unavailable` だけはトップレベル（前述）

### providers/<slug>.toml

```toml
name = "Station A"
prefix = "300"
identifier = "987fcdeb-51a2-43d1-9c4e-5a6b7c8d9e0f"   # add provider で相手の mantela から取得
url = "https://station-a.example.org/.well-known/mantela.json"   # mantela の `mantela` として出力
enabled = true
publish = true
# unavailable = true      # 省略時の決まり方は前述

[trunk]                   # 無ければトランクを生成しない（mantela 掲載のみ）
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

- station.toml の `[mantela.aboutMe]` とは見た目が似ているが、形式は共用しない。aboutMe は自局が自分で決めて名乗る内容で、provider は自局から見た相手局との接続である。provider には、相手が決めた内容の写し（`name`, `identifier`）と、自局の都合（`prefix`, `enabled`, `[trunk]`）が混ざっている。特に `preferredPrefix`（相手が希望する番号の一覧）と `prefix`（自局のダイヤルプランで実際に割り当てた番号）は別物なので、同じ形式にすると区別が見えにくくなる
- 共通点は実装側で活かす。aboutMe の Go の型は station.toml の読み込みと相手の mantela.json の取得（`add provider` / `import --mantela` / `check --remote`）で共用する
- 相手の mantela.json の URL を `mantela` ではなく `url` と書くのは、素通し用の `[mantela]` テーブルと名前がぶつかるため（TOML では同じキーを値とテーブルの両方に使えない）。`add provider` はこの URL を取得に使う
- `accept` は1局につき1認証の場合だけを扱う。共通の認証情報で複数局が登録してくる型は、どの局からの呼かを区別できず prefix で振り分けられないため、当面は対象外
- 相手局がフル番号を受け付ける場合は `strip_prefix = false`。番号の付け替えが必要な場合は `prepend` を使う

`enabled` と `publish` の関係は次のとおり。

| `enabled` | `publish` | Asterisk | mantela |
|---|---|---|---|
| true | true | トランク・発信ルートを生成 | 掲載 |
| true | false | トランク・発信ルートを生成 | 載せない |
| false | true | 生成しない | 掲載し `unavailable = true`（前述の決まり方による） |
| false | false | 生成しない | 載せない |

### PBX 依存の境界

- mantela 側（`[mantela.aboutMe]`、要素ファイルのトップレベルの mantela 項目と `[mantela]`、`kind` / `members`）は PBX に依存しない
- Asterisk 固有のものは `[asterisk]`、`[pjsip.*]`、`[trunk]`（`username` / `contact_user` など）、`dialplan` の `target`（Asterisk の `context,exten,priority` 書式）に閉じ込める
- 他の PBX への対応は予定しない。必要になったら、この境界の Asterisk 側だけを PBX ごとに用意する（要素ファイル側も手直しが要る）

## パスワード

- **パスワードは PBX 上の1ファイルだけに置く**。生成物・局ディレクトリには一切入らないので、生成物を commit / PR / CI で扱っても安全
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

- テンプレートは参照より先に定義されている必要があるため、読み込み順は道具が保証する。`pjsip_mantela.conf` の先頭で `station.toml` の `[asterisk] secrets` を `#include` する。利用者が `pjsip.conf` に足すのは `pjsip_mantela.conf` の1行だけで、順序を間違えることはない
- `build` は `secrets.required`（必要なテンプレート名の一覧）も出力。局ディレクトリが無い PBX 上でも `secret check` で不足を検出できる
- 反映の順序が重要で、secrets ファイルやテンプレートが無いと pjsip 設定全体の読込が失敗するため、`secret check` が通ってから `pjsip reload` する
- 発行するパスワード（内線、`accept` トランク）は `secret gen` で生成し一度だけ表示。相手局から受け取るもの（`register` トランク）は `secret set` で非表示入力

## CLI

```
mantela init [DIR]                     # station.toml と defaults.toml（コメント付きの雛形）を作る
mantela add extension [NUM] [--name] [--kind] [--type] [--identifier] [--private] ...
mantela add provider [MANTELA_URL] [--slug] [--prefix] [--mode] [--host] ...
mantela rm  <extension NUM | provider SLUG>
mantela list
mantela check                          # 整合性 + mantela スキーマ検証
mantela build [--mantela FILE] [--asterisk DIR]
mantela build --check                  # 出力が変わるなら非0終了
mantela secret gen|set|check|list ...  # パスワードファイルを扱う（通常 PBX 上）
mantela import [--asterisk PJSIP_CONF] [--mantela URL|FILE] ... DIR   # 初回移行用（後述）

# v1 では実装しない（後述「v1 の範囲」）
mantela set <extension NUM | provider SLUG> KEY=VALUE ...
mantela check --remote [SLUG ...]      # 相手局の mantela.json と照合（通信あり）
```

- 引数が足りず端末（TTY）なら対話で補う。非 TTY ならエラー（スクリプトから安全に呼べる）
- `add provider` は相手の mantela.json から name / identifier / preferredPrefix を補完
- 出力は決定的（同じ入力なら同じ出力。改行固定）。stdout は結果、stderr は警告

### check の内容

- 内線番号・identifier・prefix の重複、prefix の前方一致の重なり（警告）、内線番号と prefix の衝突（警告）
- mantela type の列挙外、identifier の文字種（mantela スキーマ: `^[\x21-\x7E]+$`）
- `group` の members 欠落・存在しない内線の参照、`dialplan` の target 欠落
- members が非公開内線を含むのに group が公開されている
- `[trunk] mode` ごとの必須項目（`register` / `ip` の `host` など）
- 道具が生成するキーを `[pjsip.*]` に書いた、トップレベルと `[mantela]` に同じキーを書いた（前述）
- 公開する要素の必須フィールドと、素通し分を含む出力全体の mantela スキーマ検証（`[mantela] version` のスキーマで）

### check --remote（相手局との照合。v1 外）

provider の `name` / `identifier` は相手の aboutMe の写しなので、時間が経つとずれる。各 provider の `url` から mantela.json を取得して照合し、次の場合に警告する。

- `identifier` が一致しない（URL の差し替えミス、相手局の作り直しなど）
- `name` が変わった
- 自局の `prefix` が相手の `preferredPrefix` に含まれていない（意図的な場合もあるので警告に留める）
- 相手の providers に自局（`[mantela.aboutMe] identifier`）が載っていない（相手側が接続を未設定の可能性）
- 取得に失敗した、または取得した内容がスキーマに合わない

- 通信が発生するので、通常の `check` とは分けて明示的に実行する。`build --check` にも含めない
- `url` の無い provider は対象外。SLUG を指定すればその provider だけを照合する
- 警告だけで局ディレクトリは書き換えない。CI で定期的に回す使い方を想定

## 生成物

```
<asterisk-dir>/
├── pjsip_mantela.conf        # 先頭で secrets を #include。endpoint / aor / auth / registration / identify
├── extensions_mantela.conf   # 下記コンテキスト
└── secrets.required
```

利用者は自分の `pjsip.conf` / `extensions.conf` に `#include` を1行ずつ足すだけ。

### ダイヤルプランの持ち方

生成するコンテキストは利用者のダイヤルプランから `include =>` される部品に徹する。

| コンテキスト | 中身 |
|---|---|
| `mantela-internal` | 内線・group・dialplan |
| `mantela-outbound` | `enabled` な provider ごとに `_<prefix>X.` → `strip_prefix` / `prepend` を適用して `Dial(PJSIP/<番号>@<trunk>)` |
| `mantela-phones` | `include => mantela-internal` + `include => mantela-outbound`。内線 endpoint の既定 context |
| `mantela-from-<slug>` | `include => mantela-internal`。トランク endpoint の既定 context。将来 `transit = true` なら `include => mantela-outbound` も |

- 単純な局: 既定の context のまま使えば、`#include` 2行だけで動く
- 独自の発信規則（外線・特番等）を持つ局: defaults.toml の `[extension.pjsip.endpoint] context = "from-phones"` などで利用者が所有するコンテキストを指定し、そこで部品を include する

  ```ini
  ; extensions.conf（利用者が書く）
  [from-phones]
  include => mantela-internal
  include => mantela-outbound
  exten => _0X.,1,Dial(PJSIP/${EXTEN}@carrier)   ; 局独自の規則
  ```

- トランクからの着信も同様に、`providers/<slug>.toml` の `[pjsip.endpoint] context`（全トランク共通なら defaults.toml の `[provider.pjsip.endpoint] context`）で利用者のコンテキストへ向けられる
- 中継（transit）は v1 では実装しない。他局からの着信は自局の内線にだけ届く

## 反映の例（道具の外）

```sh
mantela build --asterisk out/ --mantela mantela.json
rsync -a out/ pbx:/etc/asterisk/mantela/
ssh pbx 'mantela secret check /etc/asterisk/mantela/secrets.required && asterisk -rx "pjsip reload" && asterisk -rx "dialplan reload"'
cp mantela.json ../site/public/.well-known/ && git -C ../site commit -am "update mantela" && git -C ../site push
```

PBX 上で編集する運用では、同じ手順を PBX 上で完結させる。

## import（初回移行用）

既存の Asterisk の pjsip.conf と、公開中の mantela.json から局ディレクトリを作る。2つは足りない情報を補い合うので、1回のコマンドで両方を読み、照合してから書き出す。

```
mantela import [--asterisk PJSIP_CONF] [--mantela URL|FILE] [--secrets FILE] [--map ENDPOINT=SLUG ...] DIR
```

| 情報 | 取り込み元 |
|---|---|
| 内線の一覧、pjsip の設定、パスワード | `--asterisk`（pjsip.conf） |
| トランクと `mode`（`registration` があれば `register`、`identify` があれば `ip`、こちらで認証して登録を受けていれば `accept`） | `--asterisk` |
| 自局の aboutMe | `--mantela` |
| 内線の name / type / identifier / model、group（`transferTo` → `members`） | `--mantela` |
| provider の name / identifier / url / prefix（公開している providers の prefix が、そのまま自局のルーティング） | `--mantela` |

- **ダイヤルプラン（extensions.conf）は読まない**。自由に書けるので、prefix とトランクの対応やグループを確実には推測できないため。ルーティングの情報は mantela.json の providers から得る
- MikoPBX など、Asterisk を内部で使う PBX からも移行できる。それらが生成した `/etc/asterisk/pjsip.conf` を読めばよく、製品ごとの処理（DB の読み込みなど）は持たない
- 書き出し先 DIR は空でなければ拒否する（既存の局ディレクトリへのマージはしない）。どちらか片方だけの指定でもよい

#### 照合

- 内線: endpoint 名と mantela の内線番号で照合する。endpoint 名が番号でなければ、TTY なら対話で聞き、非 TTY なら警告して飛ばす
- トランクと provider: 自動では結びつけない。TTY なら対話で選び、非 TTY なら `--map ENDPOINT=SLUG` で指定する。結びつかなかったものは別々のファイルとして書き出し、`check` のエラーで知らせる
- mantela にあって endpoint が無い内線は、type が `application` 等なら `kind = "dialplan"`（`target` は空のまま。`check` で知らせる）、それ以外は `kind = "phone"` として警告する
- endpoint があって mantela に無い内線は `kind = "phone"`, `publish = false`

#### pjsip.conf の扱い

- `;` のコメント、テンプレート `(!)` とその継承、`(+)`、`#include` を解決してから読む
- 道具が生成するキー（前述）は取り込み時に消費し、残りのキーは書いた順のまま `[pjsip.*]` に入れる。設定が移行で失われない
- 全内線（または全トランク）で同じ値のキーは defaults.toml の `[extension.pjsip.*]` / `[provider.pjsip.*]` にまとめる
- トランクの認証ユーザー名・contact_user は `[trunk]` の `username` / `contact_user` として保持する（相手局の設定を変えさせないため）
- **パスワードは `--secrets` で指定したファイルに secrets 形式で書き出す**。局ディレクトリの中には決して書かない。パスワードが見つかったのに `--secrets` が無ければエラーにする
- transport やグローバル設定は取り込まない（利用者の pjsip.conf に残す）

取り込み後は自動で `check` を実行し、手で直すべき箇所（`dialplan` の `target`、結びつかなかったトランクなど）を一覧にする。

## v1 の範囲

v1 は「局ディレクトリから mantela.json と Asterisk 設定を作り、安全に反映できる」ところまで。

| 実装する | 実装しない（設計は残す） |
|---|---|
| `init` / `add` / `rm` / `list` | `set`（ファイルは人間が直接編集する前提なので、なくても困らない） |
| `check` / `build` / `build --check` | `check --remote`（あると便利だが運用は回る） |
| `secret gen` / `set` / `check` / `list` | `transit`（局間ループ対策が未決のため） |
| `import`（`--asterisk` / `--mantela`。初回移行に必須） | 接続状況からの `unavailable`（後述「将来案」） |

## 将来案

### 接続状況を unavailable に反映する

mantela の `unavailable` は「（一時的に）利用できないとき true」なので、Asterisk の接続状況を反映する考え方とは合っている。状態は次のように取れる。

| 対象 | 判定の材料 | 取り方 |
|---|---|---|
| トランク `register` | 自局からの登録の成否 ＋ OPTIONS への応答 | `pjsip show registrations` / AMI `PJSIPShowRegistrationsOutbound` |
| トランク `accept` | 相手が登録しているか ＋ OPTIONS への応答 | `pjsip show contacts` / AMI `PJSIPShowContacts` |
| トランク `ip` | OPTIONS への応答（aor に静的な contact と `qualify_frequency` が要る） | 同上 |
| 内線 | 電話機が登録しているか | 同上 |

- `asterisk -rx` の出力は Asterisk のバージョンで形式が変わるので、読み取る処理がおかしくなりやすい。AMI は構造化されたデータが取れるが、manager.conf に認証情報が要る
- **`build` には混ぜない**。`build` は局ディレクトリだけから決定的に作る性質（`build --check` や CI での差分確認）を保つ。別のコマンド（例: PBX 上で cron から `mantela status apply mantela.json`）が、出来上がった mantela.json の `unavailable` だけを書き換える形にする
- 一瞬の切断でばたつかないよう、「N 回続けて失敗したら true」のようなしきい値を設ける
- 状態が変わるたびに公開し直すことになるので、いまの「サイトのリポジトリに commit / push して公開」という経路とは相性が悪い。PBX から直接配信するなど、公開経路の見直しとセットで検討する

## 未決事項

- [ ] 共通認証で複数局が登録してくる型の受け側トランク（当面対象外。需要があれば identify の方法から検討）
- [ ] 発信ルートで、prefix 単位の `strip_prefix` / `prepend` で表せない規則をどこまで扱うか
- [ ] 中継（transit）を入れる場合の局間ループ対策
- [ ] transport の扱い（利用者定義の transport 名を参照するだけにするか）
- [ ] CLI ライブラリ（flag / cobra）、ライセンス
- [ ] mantela の将来バージョン（0.2.x 等）への追従方法。項目の追加は `[mantela.aboutMe]` と要素ファイルの `[mantela]` の素通しで吸収できる。構造が変わる場合に `[mantela] version` で出力を切り替えるかは未定
- [ ] 生成した設定のテスト。案は3層で、golden テスト（`build` の出力と mantela スキーマの検証。Asterisk 不要）、読み込みテスト（Asterisk 1台に読み込ませ、エラーが無いことと endpoint・コンテキストを確かめる）、通話テスト（2〜3台で `[trunk] mode` ごとの登録と着信を確かめる）。Asterisk の公式 Docker イメージは無いので、公式ソースから自前でビルドするイメージを用意し、LTS の複数バージョンで回す
