# İsteğe bağlı meeting ve ortak STT/TTS dağıtımı

Bu entegrasyon SaaS ve on-prem için aynı `spartaskctl config → .env düzenleme → restart`
akışını kullanır. Eşleşen backend/web sürümü, meeting migration'ları ve güncellenmiş Daily modülü
ayrıca gereklidir. Kurulu eski Daily paketini veya yayımlanmış imajları bu değişiklik kendisi güncellemez.
Bu belge, feature branch'inde henüz yayımlanmamış dağıtım desteğini açıklar.

## Hangi alanları dolduracağım?

Aynı sunucuda LiveKit bağlantısını kısa adımlarla kurmak için:

```sh
sudo ./spartaskctl config -meeting
# Konum: local
# Toplantı adresi: meetings.sizin-domaininiz.com (veya wss://…)
# IP: istemcilerin medyayı gönderdiği gerçek sunucu IPv4 adresi
# İmaj: sürüm profilindeki repository@sha256:<digest> (bilmiyorsanız boş; servis kapalı kalır)
sudo ./spartaskctl config -meeting -check
sudo ./spartaskctl restart
```

Bu sihirbaz LiveKit bağlantısını hazırlar; **STT/TTS ve ajan sesi ayarlarını değiştirmez**.
`local` ve `external` seçimleri medyayı açık ister; `config` container çalıştırmaz, yalnız `restart`
uygular. Eksik alan varsa feature etkili olarak kapalı kalır. `off`, medya ve ajan sesini kapatır;
başka süreçlerin kullandığı ortak `SPEECH_ENABLED` ayarı korunur. EOF/iptalde dosyalara yazılmaz.

| Ayar grubu | Yapacağınız işlem |
|---|---|
| `LIVEKIT_URL` | Kullanacağınız meeting domainini girin; DNS kaydı ve güvenilir TLS ayrıca hazırlanır. |
| `LIVEKIT_PUBLIC_IP` | Medyanın ulaşacağı sunucu IPv4 adresini girin. Cloudflare proxy IP'si kullanılmaz. |
| `LIVEKIT_IMAGE` | Seçilen/sınanmış LiveKit sürümünün digest'ini dağıtım profilinden alın. Rastgele hash veya `latest` yazmayın. |
| `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | Yerel kurulumda boş bırakın: `config` üretir ve mevcut dolu değerleri değiştirmez. Haricide gerçek servis anahtarlarını girin. |
| `LIVEKIT_INTERNAL_URL` | Yerelde boş bırakılabilir; çalışma tanımında WSS adresinin HTTPS karşılığı türetilir. |
| `LIVEKIT_TLS` | Mevcut TLS ayarı meeting domainini kapsıyor ve güvenilir ise boş kalabilir. Aksi halde ör. ACME e-postası veya uygun sertifika ayarı gerekir. |
| TCP/UDP port, CPU/RAM, concurrency ve oda limitleri | Gelişmiş ayarlar: ilk pilotta mevcut varsayılanlarla başlayın; kapasiteyi ölçmeden büyütmeyin. |
| `SPARTASK_VOICE_IMAGE` | Backend yayınının Actions özetindeki digest değerini alın; LiveKit imajı değildir. |
| STT/TTS `MODEL`, `IMAGE`, `ENV_FILE`, `PROVIDER_REVISION`, ses profili | Kullanılan üretim/model profilinin birlikte verdiği değerlerdir. Kullanıcıdan model adı veya hash uydurması beklenmez. |
| STT/TTS `API_KEY` | Gerçek model servisindeki anahtarla aynı olmalı; farklı bir rastgele değer üretmek sunucuyu yapılandırmaz. |

Yeni kurulum şablonunda `LIVEKIT_MANAGED=true`, medya flag'i ise **false**'tur. Bu sayede plain
`config` de boş yerel key/secret üretir ama container oluşturmaz. Mevcut dosyadaki açık `false`
değeri korunur; ekrandaki key/secret'in boş kalması bunun sonucudur. Aynı sunucuda kuracaksanız
mevcut sürümde de `LIVEKIT_MANAGED=true` yazıp `config` çalıştırmanız key/secret üretir.
Eski bir dosyada endpoint var fakat sahiplik anahtarı yoksa bağlantı harici olarak korunur.

Sihirbazda sunucu sahipliği yerelden hariciye veya tersine değiştirilirse eski sunucunun
kimlik bilgileri yeni sunucuya taşınmaz: yerelde yeni değerler üretilir, haricide gerçek anahtarlar
istenir. Aynı konumda tekrar çalıştırmak anahtarları döndürmez. Secret değerleri rapora yazılmaz.

İmaj digest'leri rastgele üretilemez. Elinizde zaten bulunan bir sürüm için
`docker image inspect <image:version> --format '{{index .RepoDigests 0}}'` digest'i gösterebilir;
imaj yerelde yoksa bu komutun sonucu varsayılmaz. Voice digest'i backend yayınından gelir.
Önceki yerel LiveKit geliştirme kabullerinde kullanılan **1.13.11** referansı:

```env
LIVEKIT_IMAGE=livekit/livekit-server@sha256:25cd902de7cacbd5f1f779f6a6ac1eae20ba8f56b0935934ce6b0ab80d1e3d20
```

Bu, yerel testte kullanılan sabit sürümdür; yeni bir üretim/kapasite/lisans kabulü iddiası değildir.
Şablona otomatik atanmaz; operatörün sürüm seçimi korunur.
Yerel STT/TTS için yeniden üretilebilir/onaylı üretim imajı ve model/voice lisans kabulü hâlâ ayrı
bir iş kalemidir; deney container'ları otomatik üretim profili yapılmamıştır. Dolayısıyla bu rehber,
şu an bütün AI ses kurulumunun yalnız üç alanla tamamlandığını iddia etmez.

## Kullanıcı akışı

```sh
sudo ./spartaskctl config
# .env içindeki açıklamalı ayarları düzenleyin.
sudo ./spartaskctl config -check
sudo ./spartaskctl restart
sudo ./spartaskctl status
```

`config` yalnız ayarları/gerekli türetilmiş dosyaları yazar. `config -check` dosya yazmaz;
mevcut advisor'lar sunucuyla ilgili öneriler için DNS/metadata/veritabanı okuyabilir. İki komut da
servis başlatmaz veya imaj/model indirmez. `restart`, Compose tanımını doğrulayıp uygular.
İlk imaj/model indirme yalnız etkin yerel servisler başlatılırken gerçekleşebilir.

| İstenen özellik | Açılacak anahtarlar | Yerel container |
|---|---|---|
| İnsanlarla görüntülü toplantı, ajanlardan metin | `MEETING_MEDIA_ENABLED=true` | `LIVEKIT_MANAGED=true` ise `livekit` |
| Meeting dışında da kullanılan dosya STT/TTS executor'ları | `SPEECH_ENABLED=true` | İlgili `SPEECH_STT_MANAGED` / `SPEECH_TTS_MANAGED` true ise model servisi |
| Toplantıda ajanlarla sesli görüşme | Üç anahtar da true | `voice` ve seçilen yerel model/SFU servisleri |

`*_MANAGED=false`: Adresini verdiğiniz harici servise bağlanılır; yerel karşılığı oluşturulmaz.
STT ve TTS bağımsız olarak yerel/harici seçilebilir. `voice` bu stack tarafından yönetilir.
STT/TTS meeting'e özel değildir; `SPEECH_ENABLED=true` bırakılırsa ajan sesi kapalı olsa da ortak
dosya executor'ları ve bunlara hizmet eden yerel modeller çalışmaya devam eder.

### Kapalı, eksik ve hatalı ayarlar

- Ana anahtar false: Diğer değerler dolu olsa da özellik kapalıdır.
- Ana anahtar true ama bir zorunlu özellik alanı boş: Rapor eksik **alan adlarını** listeler;
  özellik etkili olarak false olur. Ana uygulama yeniden başlayabilir. Örneğin TTS anahtarı boşsa
  STT/TTS ve ona bağımlı voice birlikte kapanır, yapılandırılmış video açık kalabilir.
- Dolu fakat hatalı adres, güvensiz transport, bellek/CPU sınırı veya sabitlenmemiş imaj:
  `restart`/`update` çalışan container'lara dokunmadan hata verir. Düzeltin veya özelliği açıkça kapatın.
- Bir servis başlamaz/sağlıklı olmazsa komut hata verir; başarılı dağıtım raporlamaz.
  `update` mevcut davranışıyla sürüm ayarını geri alır; otomatik veritabanı geri yüklemesi yapmaz.

İstenen anahtarlar `.env`'de korunur. Etkili anahtarlar bütün API/worker/scheduler süreçlerine
`compose.meeting.generated.yml` aracılığıyla aynı değerle geçirilir. CLI bu dosyayı `COMPOSE_FILE`'a
bir kez ekler. Elle düzenlemeyin. Dosya servis anahtarlarını içerir; mode 0600 yazılır, Git'e girmez
ve yapılandırma yedeğine dahildir. `COMPOSE_PATH_SEPARATOR=:` Linux/Windows biçimini eşitler.
Terminaldeki `COMPOSE_FILE` / `COMPOSE_PROJECT_NAME` gibi dış ortam değişkenleri Docker'ın normal
öncelik kurallarıyla `.env`'yi geçersiz kılabilir; işletimde bunları farklı bir stack'e ayarlamayın.

`restart` mevcut `up -d --remove-orphans` davranışını kullanır. Artık tanımlanmayan bu isteğe bağlı
servisler kaldırılır. **Önce aktif toplantıları bitirin ve speech işlerini tamamlayın.** Kapama aktif
işleri kesebilir. Model volume'ları ve indirilmiş imajlar otomatik silinmez; CPU/RAM tüketmezler,
ancak diskte önbellek olarak kalırlar. Bu mekanizma toplantı başına otomatik uyutma değildir:
özellik açıkken modeller sonraki istekler için yüklü kalır.

## LiveKit

Her modda doldurun: `LIVEKIT_URL=wss://<meeting-domain>`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET` (en az 32 karakter). Harici modda ayrıca `LIVEKIT_INTERNAL_URL=https://…`.
Harici SFU **`room.auto_create: false`** kullanmalıdır; eski meeting biletlerinin kapalı odayı
yeniden oluşturamaması bununla sağlanır. Herhangi bir bulut ürününün bu ayarı desteklediği varsayılmaz.

Yerel modda `LIVEKIT_MANAGED=true`, `LIVEKIT_IMAGE=<onaylı-repo>@sha256:<digest>`,
`LIVEKIT_PUBLIC_IP=<istemcilerin eriştiği IPv4>` ekleyin. API anahtarları boşsa `config` güçlü,
rastgele değerler üretir; tekrar çalıştırmak mevcut değerleri değiştirmez. Harici sunucu için anahtar
üretilmez: o sunucunun gerçek servis anahtarları girilir.

Yerel mod, ayrı meeting alan adı için Caddy yönlendirmesi ve `LIVEKIT_CONFIG` üretir; oda otomatik
oluşturma kapalıdır. Domain uygulamanın kök adresinden farklı, tenant adresleriyle çakışmayacak
şekilde ayrılmış olmalıdır. `LIVEKIT_INTERNAL_URL` boşsa WSS adresinden HTTPS karşılığı türetilir.
İç Docker ağında bu domain Caddy'ye alias olur; TLS/SNI korunarak public-IP hairpin NAT ihtiyacı azaltılır.

DNS meeting adresini sunucuya yönlendirmeli; Cloudflare kullanılıyorsa SFU'nun medya trafiğini HTTP
proxy'siyle taşıyacağınızı varsaymayın. `LIVEKIT_TLS` için mevcut sertifika dosyaları veya ACME
e-postası seçilebilir. Boşsa `SPARTASK_TLS` kullanılır; **sertifika meeting domainini kapsamalı ve hem
tarayıcılar hem API/voice container'ları tarafından güvenilir olmalıdır**. Cloudflare Origin
sertifikası doğrudan tarayıcı erişimi için yeterli değildir. `internal` / şirket CA'sında istemciler
ve uygulama container'larının güven deposunu ayrıca hazırlayın; TLS doğrulaması kapatılmaz.

Varsayılan WebRTC portları TCP 7881 ve UDP 7882'dir; host'ta aynı portlar yayımlanır. Güvenlik duvarı,
bulut security group ve varsa NAT yönlendirmesi gerekir. HTTPS/WSS Caddy 443 üzerinden gider;
LiveKit API 7880 host'ta yayımlanmaz. Model portları da host'a açılmaz.

Bu yerel profil **TURN sunucusu kurmaz**. Kısıtlı şirket ağları için TURN/TLS ayrıca tasarlanmalı
veya `LIVEKIT_MANAGED=false` ile TURN'ü yapılandırılmış bir SFU kullanılmalıdır. Windows Hyper-V
kurucusunun mevcut 80/443 yönlendirmesi bu yeni TCP/UDP portlarını otomatik yönlendirmez;
bu ortamda ağ yönlendirmesi ayrıca hazırlanmalı ya da harici SFU seçilmelidir.

Resmi ağ gereksinimleri: [LiveKit deployment](https://docs.livekit.io/transport/self-hosting/deployment/),
[ports/firewall](https://docs.livekit.io/transport/self-hosting/ports-firewall/).

## STT/TTS sağlayıcı sözleşmesi

### Yayımlanan yerel İngilizce profili

Dağıtım paketinin `speech-profile.json` dosyası `english-cpu-v1` için yayımlanmış STT, TTS ve
voice imajlarının değişmez digest referanslarını içerir. Etiket veya kullanıcı tarafından uydurulan
hash kabul edilmez. Dosya yoksa ya da profil geçersizse kurulum hata verir; otomatik imaj seçmez.

```bash
./spartaskctl config -speech-local
./spartaskctl config -check
./spartaskctl restart
```

İlk komut açık bir sahiplik değişimidir: mevcut harici/özel STT/TTS adreslerini temizleyip iki servisi
bu stack'e taşır; sağlayıcı ve sağlık kontrolü için eşleşen, bağımsız anahtarlar üretir. Aynı profili
yeniden seçmek mevcut anahtarları döndürmez. Düz `config`, `config -meeting` veya güncelleme
harici sağlayıcıyı bu profile geçirmez. `config -speech-local -check` yalnız mevcut ayarları raporlar;
profil seçmez, anahtar üretmez ve dosya yazmaz.

Profil İngilizce `whisper-base.en`, `kokoro-en` ve `af_heart` kullanır; CPU thread sayıları STT=2,
TTS=2'dir. Başlangıç container sınırları STT 2 CPU/1g ve TTS 2 CPU/2g, ortak inference eşzamanlılığı
1'dir (STT ve TTS aynı anda model çalıştırmaz). Bunlar ölçülmüş kapasite veya minimum RAM garantisi değildir. İngilizce dışı ihtiyaçlarda özel
sağlayıcı yapılandırın ve `SPEECH_LOCAL_PROFILE` alanını boş bırakın. Bilinmeyen profil değerleri ve
paketle uyuşmayan model/imaj ayarları geçersizdir.

Bundled servisler anahtarı doğrudan `SPEECH_SERVICE_API_KEY` olarak alır; ayrıca özel `*_ENV_FILE`
hazırlanmaz. Salt okunur kök dosya sistemi ve `/tmp` tmpfs kullanırlar; model dosyaları imajın içindedir.
TTS'nin eSpeak kütüphanesi geçici paylaşımlı kütüphaneyi `/tmp` üzerinden yüklediğinden yalnız bu
servisin tmpfs'i `exec,nosuid` kullanır; STT'de `noexec,nosuid` korunur.
Host portu veya model indirme volume'ü açılmaz. Komut yalnız yapılandırır; Docker/imaj indirme ve
servis başlatma `restart` sırasında gerçekleşir. Ajan sesi ayrıca tamamlanmış LiveKit ayarları ister.

### Özel veya harici sağlayıcılar

Ortak alanlar: `SPEECH_PROVIDER_REVISION`, iki sağlayıcının `*_MODEL`, `*_API_KEY` alanları ve
`SPEECH_TTS_VOICE`. Harici sağlayıcılarda OpenAI uyumlu `*_URL` base adresi (`/v1` dahil) girilir.
HTTPS tercih edilir; HTTP yalnız özel/loopback IP veya yönetilen Docker DNS adlarında kabul edilir.

Yerel sağlayıcıda `SPEECH_<STT|TTS>_MANAGED=true`, `*_IMAGE`, `*_ENV_FILE` doldurulur. URL boşsa
`http://speech-stt:8000/v1` veya `http://speech-tts:8000/v1` türetilir. İmaj sözleşmesi:

1. Onaylanmış, digest'e sabitlenmiş CPU imajı; giriş komutu imajın kendi ENTRYPOINT/CMD'sidir.
2. STT: multipart `POST /v1/audio/transcriptions`. TTS: `POST /v1/audio/speech`, WAV yanıtı.
3. Port 8000'de dinleme ve bearer anahtar doğrulaması. Model/ses/anahtar ayarları imaja özgü
   `*_ENV_FILE` içinde açıkça hazırlanır; rastgele bir vendor imajının ayar adları tahmin edilmez.
4. Model yüklenip ısındıktan sonra `GET /health` başarılı olmalı. Sağlık kontrolü Python 3 stdlib
   kullanır ve `SPARTASK_HEALTH_KEY` ile bearer header gönderir. Bu değişken yalnız probe içindir;
   imajın gerçek kimlik doğrulama ayarı ayrıca aynı anahtarla yapılmalıdır.
5. Model cache `/models` altında olmalı. İmaj bu dizini çalıştığı UID için yazılabilir hazırlamalı;
   named volume ilk kullanımda bu sahipliği devralır. Dizin tek başına modeli önceden indirmez.

Özel env dosyalarını `.secrets/stt.env` / `.secrets/tts.env` gibi yedeklenen yerde saklayın; dışarıdaki
env dosyaları otomatik yedeklenmez. Model volume'ları uygulama yedeğine dahil edilmez; cache tekrar
indirilebilir olmalı, özel modeller ayrıca yedeklenmelidir.

Özel yerel deney imajları otomatik seçilmez veya registry'ye gönderilmez.
Model/voice/vocoder/OS/Python lisans ve zafiyet envanteri,
özellikle müşteriye on-prem dağıtım öncesinde tamamlanmalıdır. İmaj alanı boşken ilgili model çalışmaz.

## Voice ve kapasite

`SPARTASK_VOICE_IMAGE` eşleşen backend sürümünün native, digest'e sabitlenmiş imajıdır. API/worker
imajını burada kullanmayın. Voice minimum gerekli meeting/speech ayarlarını ve türetilmiş DB adresini
alır; uygulamanın diğer şifrelerini/volumelerini almaz. Modellerin hazır olmasını ve migration'ın
başarılı bitmesini bekler. GPU varsayılmaz; bütün süreçlerin kotası API/worker/voice'ta aynı olmalıdır.

Pilot varsayılanı tek sesli oda, firma başına tek oda, dört insan mikrofonu ve ortak tek inference'dır.
CPU/bellek üst sınırları `.env`'de ayrı ayrı değiştirilebilir. Bunlar rezervasyon veya kapasite garantisi
değildir. Özellikle mevcut 4 CPU / 8 GB sunucuda model limitleriyle birlikte uygulama/DB/OS yükü
ölçülmelidir. TTS için 3g üst sınırı yerel deney profilinden gelir; gerçek tüketim veya minimum RAM iddiası
değildir. Özellikler kapalıyken bu servislerin container'ı oluşturulmaz.

API sağlık kontrolüne ek olarak model container'larında warm `/health` kontrolü beklenir. Resmi SFU ve
mevcut voice imajında bu probe bulunmadığından yalnız kısa süre kararlı `running` durumu doğrulanır;
bu, DNS/TLS/WebRTC bağlantısını veya konuşma doğruluğunu doğrulamaz. Harici sağlayıcıların erişilebilirliği
`config` sırasında test edilmez. Her container için bekleme üst sınırı `MEETING_SERVICES_START_TIMEOUT`'tır.

## Doğrulama ve sınırlar — 15 Kasım 2026

İngilizce CPU profili için ek yerel kabul: gerçek Kokoro → PCM16/48kHz WAV → Whisper akışı,
eksik/yanlış bearer anahtarı, yanlış model, giriş limitleri, 1000 karakter sentezi ve gerçek TCP
bağlantısı kesildikten sonra modelin toparlanması toplam 16 kontrolde geçti. Sentetik 3,52 saniyelik
sesin TTS üretimi 4,8–6,1 saniye, STT çözümü 2,3–3,5 saniye sürdü; beklenen altı kelimenin tamamı
bulundu. Bunlar yerel Docker ölçümleridir. STT tepe belleği yaklaşık 465 MiB oldu; TTS 1 GiB
sınırında bellek baskısı gördüğünden profil üst sınırı 2 GiB seçildi. Nihai 2 GiB/tek CPU kabulü
16/16 geçti; TTS tepe belleği yaklaşık 1,36 GiB, 1000 karakter/57,24 saniye ses üretimi 55,22
saniye oldu. İmaj inference süresi 75 saniye ile sınırlıdır. VPS kapasitesi ve gerçek
mikrofon doğruluğu ayrıca ölçülmelidir. Yerel imaj boyutları TTS yaklaşık 1,46 GB, STT 1,12 GB'dır;
ortak katmanlar paylaşılır. Derleme cache'i çalışma zamanı gereksinimi değildir.

- Windows ve Linux Go testleri / `go vet` çalıştırıldı. Linux kurulum/güncelleme testleri ağsız,
  geçici container'da fake Docker ile yürütüldü.
- Özellik matrisi: varsayılan kapalı, ayarlar doluyken kapalı, eksik anahtar, yalnız video,
  yalnız ortak speech, harici servisler, yerel modeller ve voice bağımlılıkları kapsanır.
- Gerçek Docker Compose 2.33.1 ile rastgele ayrı projede `kapalı → dört servis açık → gerekli ayar
  eksik/medya kapalı → dört servis kaldırıldı` geçişi geçti. Uygulama fixture'ları çalışır kaldı;
  test container/ağ/volume'ları temizlendi. Model/SFU yerine inert Debian süreçleri kullanıldı;
  gerçek AI/ses veya üretim modeli kabulü olduğu iddia edilmez.
- Digest zorunluluğu, güvensiz URL/TLS girişi, secret çıktısı, `$` interpolation, DB şifresi encoding,
  config dry-run, anahtarların korunması ve Caddy mount kaldırılması kontrol edildi.
- Son gerçek Compose geçişi 60.88 saniyede geçti. API/voice container ortamları ve SFU'nun iç JSON
  yapılandırmasında `$` içeren sentetik anahtarın aynen korunduğu ayrıca doğrulandı. `compose config`
  çıktısı round-trip için dolarları yeniden escape edebildiğinden bu kontrol gerçek container
  ortamından yapıldı. İç TLS domain alias'ı da çözümlenen Compose tanımında doğrulandı.
- Config kolaylaştırması sonrası Windows/Linux tam testleri, vet ve Windows CLI derlemesi geçti.
  Sihirbazın yerel/harici geçişi, EOF iptali, mevcut anahtarları koruması, eski harici bağlantıyı
  sahiplenmemesi, `-meeting -check` için soru/yazma olmaması ve `.env.bak` doğrulandı. Yeni varsayılanla
  gerçek Docker açma/kapama testi de **70.72s** içinde geçti; test kaynakları temizlendi. İmajlar yine
  hafif fixture süreçleridir; bu testte gerçek STT/TTS çalıştırılmadı.
- Hazır resmi LiveKit 1.13.11 binary'si üretilen aynı alanlarla ağsız `ports` komutunu kabul etti;
  bu kontrol TLS veya gerçek WebRTC bağlantısı kurmadı. Windows CLI binary'si ayrıca derlendi.

### Commit öncesi teknik borç incelemesi

`technical-debt-audit` kontrol listesiyle bu değişikliklerin incelemesinde giderilmemiş
**kritik 0 / orta 0 / düşük 0** kod bulgusu kaydedildi. Bu, bağımsız güvenlik veya lisans sertifikası değildir.

- Domain/servis/port katmanlarına bağımlılık eklenmedi; bağımsız dağıtım CLI'si kendi dosya ve Docker
  adaptörlerini kullanır. Yeni SQL/transaction veya arka plan goroutine'i yoktur.
- Docker çağrıları context taşır; readiness döngüleri süreyle sınırlıdır. Dosya yazımları mevcut atomik,
  0600 secret yazıcısını kullanır; raporlar servis anahtarlarının değerini göstermez.
- İncelemede kapama sırasında eski container'ların korunması, Compose `$` dönüşümü, Caddy'nin dosya
  değişikliğini kendiliğinden yüklememesi ve config yedeğinin üretilen tanımı kapsamaması ele alındı.
  Bunlar sırasıyla servisleri tanımdan çıkarma + orphan kaldırma, literal escaping + gerçek env testi,
  içerik hash'iyle recreation ve yedeğe ekleme ile giderildi.
- Üretim imajları/lisansları, gerçek tarayıcı/ses, TURN/Hyper-V ağı ve hedef sunucu kapasitesi yukarıdaki
  ayrı kabul koşullarıdır; bu kod incelemesi bunların tamamlandığını iddia etmez.

Tekrar çalıştırma (yerelde önceden bulunan digest-pinned Debian imajı ve Docker gerekir):

```sh
cd spartaskctl
go test ./...
go vet ./...
SPARTASKCTL_TEST_IMAGE=debian@sha256:<yerel-digest> go test -run TestMeetingDockerLifecycle -count=1 -timeout=4m -v
```

Docker testi `spartaskctl-meeting-test-<rastgele>` projesinden başka kaynak yönetmez. Fixture'da
portları kaldıran Compose `!reset` etiketi için >=2.24.4 gerekir; üretim tanımı bu etiketi kullanmaz.

Canlıya açmadan önce gerçek sertifika/DNS, mobil veri ve şirket ağı/TURN bağlantısı, gerçek
mikrofon/AI/seslendirme, onaylı karar ve kapanış/oda temizliği testi yapılmalıdır. Container'ların
başlaması bu kabulün yerine geçmez. Bu değişiklik canlı sunucuyu, yayımlanmış imajları veya başka
grubun branch'ini değiştirmedi.
