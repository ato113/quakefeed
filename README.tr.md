# quakefeed

*Türkçe · [English](README.md)*

**Birbiriyle anlaşamayan üç ağdan tek bir deprem API'si.**

[![CI](https://github.com/ato113/quakefeed/actions/workflows/ci.yml/badge.svg)](https://github.com/ato113/quakefeed/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.23%2B-00ADD8)](https://go.dev)
[![Lisans: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Türkiye'deki son depremleri almak istiyorsan üç seçeneğin var ve üçü de zahmetli.
AFAD, her sayıyı string olarak JSON döndürüyor. Kandilli, ISO-8859-9 kodlu bir
`<pre>` bloğunun içinde sabit genişlikli tablo yayınlıyor. USGS temiz GeoJSON
veriyor ama Türkiye'deki merkez üslerini yerel ağlar kadar hassas konumlamıyor.
Üçü de zamanı farklı biçimde bildiriyor, farklı büyüklük ölçekleri kullanıyor ve
aynı depremi farklı sayılarla anlatıyor.

`quakefeed` üçünü birden tarar, aynı depremin raporlarını tek bir olayda
birleştirir ve sonucu düz JSON olarak sunar. API anahtarı yok, bağımlılık yok,
tek binary.

```console
$ curl 'localhost:8080/v1/events?min_mag=3&since=24h'
{
  "count": 2,
  "total": 2,
  "events": [
    {
      "id": "1a6a5ace8b727913",
      "time": "2026-10-31T14:58:05Z",
      "latitude": 39.451,
      "longitude": 28.102,
      "depth_km": 10.1,
      "magnitude": 3.6,
      "mag_type": "ML",
      "place": "Sındırgı (Balıkesir)",
      "sources": ["afad", "kandilli", "usgs"]
    }
  ]
}
```

Buradaki tek olay aslında üç ayrı rapor — AFAD'ın, Kandilli'nin ve USGS'in —
aynı deprem olarak tanınıp birleştirilmiş hali.

## Çalıştırma

### Docker ile (başka hiçbir şey kurman gerekmez)

```bash
git clone https://github.com/ato113/quakefeed
cd quakefeed
docker compose up
```

### Go ile

Go 1.23 veya üstü gerekiyor — `go version` sende ne olduğunu söyler.

```bash
git clone https://github.com/ato113/quakefeed
cd quakefeed
go run ./cmd/quakefeed
```

İstediğin yere kopyalayabileceğin bir binary üretmek için:

```bash
go build -o quakefeed ./cmd/quakefeed
./quakefeed
```

### Çalıştığını kontrol et

Her iki yolda da `:8080` portunu dinler ve dakikada bir yeniler. İlk yenileme
açılışta olur, birkaç saniye bekleyip şunları dene:

```bash
curl localhost:8080/healthz          # durum, olay sayısı, çalışma süresi
curl localhost:8080/v1/sources       # hangi kaynak cevap veriyor
curl 'localhost:8080/v1/events?min_mag=3&since=24h'
```

`/v1/sources` bir kaynağı sağlıksız gösteriyorsa `last_error` alanı nedenini
yazar. Servis, cevap veren kaynaklarla çalışmayı sürdürür.

## İşin ilginç kısmı: birleştirme

Aynı deprem hiçbir zaman iki kez aynı görünmez. Yukarıdaki olay için üç ağın
bildirdikleri şunlardı:

| Kaynak | Zaman (UTC) | Merkez üssü | Büyüklük |
|--------|-------------|-------------|----------|
| AFAD | 14:58:05 | 39.451, 28.102 | 3.6 ML |
| Kandilli | 14:58:12 | 39.460, 28.120 | 3.4 ML |
| USGS | 14:58:18 | 39.440, 28.090 | 3.7 mb |

İki rapor, birbirinden **45 saniye ve 80 km içindeyse** aynı deprem sayılır. Bu
sayılar bilinçli: daha dar olsa aynı deprem üç ayrı olaya bölünürdü, daha geniş
olsa gerçek bir artçı ana sarsıntının içinde kaybolurdu.

Birleştirmeyi dürüst tutan üç kural var:

- **Bir kaynak kendisiyle asla birleşmez.** AFAD'ın on saniye arayla verdiği iki
  kayıt bir deprem sürüsüdür, kopya değil — o yüzden tek bir kaynağın kendi
  raporları her zaman ayrı olaylar olarak kalır.
- **Yerel ağlar önde gelir.** AFAD ve Kandilli, Türkiye'deki merkez üslerini
  küresel bir ağdan daha hassas konumlar; manşet rakamlar onlardan gelir. USGS
  yine de kendi raporuyla katkıda bulunur.
- **Hiçbir sayı ortalanarak yok edilmez.** `?detailed=true`, her kaynağın kendi
  rakamlarını olduğu gibi döndürür. Farklı ölçeklerdeki büyüklükler (ML, Mw, Md,
  mb) birbirinin yerine kullanılamaz, bu yüzden ölçek her zaman sayıyla birlikte
  taşınır.

Olay kimlikleri depremin kendisinden türetilir — zaman dakikaya, koordinatlar
yaklaşık bir kilometreye yuvarlanarak — herhangi bir kaynağın kimliğinden değil.
Bir olay, yenilemeler arasında, bir kaynak rakamlarını hafifçe revize ettiğinde
ve onu hangi kaynakların gördüğünden bağımsız olarak aynı kimliği korur.

## Bir kaynak çöktüğünde

Bunlar herkese açık servisler ve çöküyorlar. Başarısız bir yenileme hiçbir zaman
veriyi silmez: kaynak son iyi gözlemlerini korur ve katalog elde kalanlardan
yeniden kurulur.

`/healthz`, bazı kaynaklar çalışırken HTTP 200 ile `degraded` döner; yalnızca
tüm kaynaklar başarısızken 503 ile `unhealthy` döner — böylece bir upstream kötü
bir öğleden sonra geçirdi diye load balancer çalışan servisi devre dışı
bırakmaz. `/v1/sources` her kaynağın son başarısını, son hatasını ve gözlem
sayısını gösterir.

## API

### `GET /v1/events`

| Parametre | Örnek | Anlamı |
|-----------|-------|--------|
| `min_mag` | `3.5` | En düşük büyüklük |
| `since` / `until` | `24h`, `7d`, `2026-10-31T00:00:00Z` | Zaman aralığı; göreli ya da RFC 3339 |
| `bbox` | `36,26,42,45` | `min_enlem,min_boylam,maks_enlem,maks_boylam` |
| `source` | `afad` | Yalnızca bu kaynağın bildirdiği olaylar |
| `limit` / `offset` | `50` / `100` | Sayfalama; `limit` en fazla 1000, varsayılan 100 |
| `detailed` | `true` | Her kaynağın kendi raporunu da ekle |

`total`, sayfalamadan önceki sayıdır; geriye ne kaldığını buradan anlarsın.

### `GET /v1/sources`

Kaynak bazında sağlık: son başarı, son deneme, son hata, gözlem sayısı.

### `GET /healthz`

`ok`, `degraded` ya da `unhealthy`; yanında olay sayısı ve çalışma süresi.

## Yapılandırma

Her bayrağın bir ortam değişkeni karşılığı var; bayrak öncelikli.

| Bayrak | Ortam değişkeni | Varsayılan |
|--------|-----------------|------------|
| `-addr` | `QUAKEFEED_ADDR` | `:8080` |
| `-interval` | `QUAKEFEED_INTERVAL` | `1m` |
| `-retention` | `QUAKEFEED_RETENTION` | `168h` |
| `-timeout` | `QUAKEFEED_TIMEOUT` | `15s` |
| `-afad-url` | `QUAKEFEED_AFAD_URL` | AFAD apiv2 latest |
| `-kandilli-url` | `QUAKEFEED_KANDILLI_URL` | KOERI liste sayfası |
| `-usgs-url` | `QUAKEFEED_USGS_URL` | USGS all-day özeti |
| `-log-level` | `QUAKEFEED_LOG_LEVEL` | `info` |

Bir kaynağın URL'sini boş bırakmak o kaynağı devre dışı bırakır.

## Kaynaklar hakkında notlar

- **Zaman.** AFAD ve Kandilli, Türkiye yerel saatini yayınlıyor ve veride saat
  dilimi bilgisi yok. Türkiye 2016'dan beri yıl boyu UTC+3 olduğundan dönüşüm
  `tzdata` yerine sabit bir offset ile yapılıyor — bu aynı zamanda konteynerin
  zoneinfo taşımasını gereksiz kılıyor. API'nin döndürdüğü her şey UTC.
- **Kodlama.** Kandilli ISO-8859-9 sunuyor. Latin-1'den tam olarak altı noktada
  ayrılıyor ve hepsi Türkçe harf; bu yüzden dönüşüm bir bağımlılık değil, küçük
  bir tablo.
- **Büyüklükler.** Kandilli Md, ML ve Mw sütunlarını yayınlıyor, çoğu zaman
  yalnızca biri dolu. Varsa Mw, sonra ML, sonra Md tercih ediliyor.
- **Parser'lar canlı trafiğe değil, kayıtlı örneklere göre yazıldı.**
  `testdata/` içindeki örnekler her parser'ın beklediği formatı belgeliyor. Bir
  kaynak düzenini değiştirirse düzeltilecek tek yer `internal/source/` — ve örnek
  dosya da onunla birlikte güncellenmeli. **Güvenmeden önce canlı veriyle bir kez
  çalıştır.**

> Can güvenliği kararları için kullanılmaz. Resmî bilgi için doğrudan AFAD ve
> Kandilli'ye başvurun.

## Geliştirme

```bash
go test ./...            # 60 test
go test -race -cover ./...
go vet ./...
```

Tüm akış `internal/e2e` içinde uçtan uca test ediliyor: üç sahte upstream
sunucu, gerçek fetcher'lar, gerçek poller ve gerçek HTTP handler'ları — dışarıda
kalan tek şey internetin kendisi.

Ayrıştırma işi bilinçli olarak veri çekmekten ayrıldı; her formatın canlı servis
yerine kayıtlı bir örnekle test edilebilmesini sağlayan şey bu.

## Katkı

Yeni bir kaynak eklemek, `internal/source/` içine `[]model.Observation` döndüren
tek bir parser ve `testdata/` içine bir örnek dosya yazmak demek. Sonrasındaki
her şey — birleştirme, filtreleme, sunma — ortak.

Yapay zekâ destekli katkılar sorun değil; yeter ki pull request'te belirt ve
gönderdiğin kodu açıklayabil.

## Lisans

MIT — [LICENSE](LICENSE) dosyasına bak.
