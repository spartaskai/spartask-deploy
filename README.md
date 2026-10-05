# Spartask Deploy

Spartask'ı bir Linux sunucuya (bulut ya da müşterinin kendi sunucusu / on-premise) kurmak ve işletmek için gereken her şey:

| Dosya | Görevi |
|---|---|
| `compose.yml` | Uygulama yığını: Caddy (HTTPS), PostgreSQL, NATS, `migrate`, API (web arayüzü dahil), worker, scheduler |
| `compose.marketplace.yml` | Marketplace (sadece platform sunucusunda, `--with-marketplace`) |
| `caddy/` | Reverse proxy ve TLS ayarı — `domain` ve tüm firma alt alan adları (`*.domain`) |
| `.env.example`, `marketplace.env.example` | Ayar şablonları (kurulumda `.env` / `marketplace.env` olarak, mode 600, üretilmiş şifrelerle yazılır) |
| `spartaskctl` | Kurulum ve işletim aracı (Go): `install`, `update`, `backup`, `restore`, `status`, `logs`, `restart` |

Uygulama imajları GitHub Actions tarafından sürüm etiketiyle üretilir (`ghcr.io/spartaskai/spartask:<sürüm>`). Sunucuda kaynak kod veya derleme yoktur; sadece imaj çekilir.

## Mimari

```
İnternet ─► Caddy :443 ─┬─► api:8080 (web + /api + /config.js)   spartask.ai, *.spartask.ai
                        └─► marketplace-web:3000 ─► marketplace-backend   (opsiyonel)
             iç ağ:  db (PostgreSQL) · nats · worker · scheduler · migrate (her açılışta bir kez)
```

- Dışarıya sadece 80/443 açılır. Veritabanı ve NATS sadece iç ağdadır.
- Her `up`/`update` öncesinde `migrate` servisi bekleyen veritabanı değişikliklerini public şemaya ve **tüm firma şemalarına** uygular; başarısız olursa API eski sürümde kalır.
- Domain hiçbir imaja gömülü değildir: web arayüzü ayarlarını çalışırken `/config.js`'ten alır. Aynı imaj her müşteride çalışır.

## Kurulum

**Gereksinimler:** Ubuntu 22.04/24.04 (2 vCPU, 4 GB RAM önerilir), Docker Engine + Compose eklentisi (`curl -fsSL https://get.docker.com | sh`), domain için `A` kaydı ve `*` (wildcard) kaydı.

```bash
sudo mkdir -p /opt/spartask && cd /opt/spartask
curl -fsSL https://github.com/spartaskai/spartask-deploy/releases/latest/download/spartask-deploy-linux-amd64.tar.gz | sudo tar xz --strip-components=1

# TLS sertifikası: domain ve *.domain'i kapsayan sertifika + anahtar (ör. Cloudflare Origin Certificate)
sudo mkdir -m 700 certs
sudo nano certs/cert.pem     # "Origin Certificate" içeriği
sudo nano certs/key.pem      # "Private Key" içeriği

sudo ./spartaskctl install
```

`install` şunları sorar: domain, sürüm, TLS modu, Cloudflare kullanımı, (opsiyonel) Gemini anahtarı ve günlük yedek görevi. Tüm şifreleri kendisi üretir, imajları çeker, migration'ları uygular ve API sağlıklı olana kadar bekler.

Örnekler:

```bash
# Platform sunucusu (spartask.ai, Cloudflare arkasında, marketplace dahil, imajlar private)
sudo ./spartaskctl install -domain spartask.ai -cloudflare -with-marketplace -registry-user <github-kullanıcısı>

# On-premise müşteri (kendi sertifikası, merkezi marketplace'e bağlı)
sudo ./spartaskctl install -domain spartask.firma.com.tr -cert /root/firma.crt -key /root/firma.key \
     -marketplace-url https://marketplace.spartask.ai

# Kapalı ağ (sertifika yok, Caddy kendi CA'sını kullanır)
sudo ./spartaskctl install -domain spartask.local -tls internal
```

> **Önemli:** `.env` içindeki `SECRET_KEY`, veri kaynağı / mail sunucusu / yapay zeka şifrelerini şifreler. Kurulumdan sonra `.env`'in bir kopyasını şifre yöneticisine koyun; bu anahtar olmadan yedekteki şifreler çözülemez.

Private imajlar için `-registry-user` ile `read:packages` yetkili bir GitHub token'ı sorulur (veya önceden `docker login ghcr.io`).

## Günlük işletim

```bash
sudo ./spartaskctl config              # .env'i bu paketin şablonuyla karşılaştır: eksikleri ekle, şifreleri üret, doldurulacakları açıkla
sudo ./spartaskctl config -check       # aynı rapor, dosyaya dokunmadan
sudo ./spartaskctl status              # sürüm, container'lar, migration durumu
sudo ./spartaskctl update 1.3.0        # yedek al → imajı çek → migrate → yeniden başlat → sağlık kontrolü
sudo ./spartaskctl update 1.3.0 -marketplace-version 1.3.0
sudo ./spartaskctl logs api            # canlı loglar (api, worker, scheduler, migrate, caddy, db ...)
sudo ./spartaskctl restart             # .env değişikliklerini uygula
sudo ./spartaskctl backup              # backups/<zaman>/ altına yedek (en yeni BACKUP_KEEP adet tutulur)
sudo ./spartaskctl restore backups/20261004T033000Z               # veriyi yedekten geri yükle
sudo ./spartaskctl restore backups/20261004T033000Z -with-config  # yeni sunucuya taşıma: .env ve sertifikalar da
```

- **Ayarlar (`.env`):** Her ayarın açıklaması ve kuralı `.env.example` şablonundadır. `spartaskctl config` sunucudaki `.env`'i şablonla karşılaştırır:
  - yeni sürümle gelen ayarları açıklamalarıyla birlikte doğru bölüme ekler (mevcut değerlere dokunmaz, önceki dosyayı `.env.bak` olarak saklar),
  - güvenle üretilebilen şifreleri üretir (`NATS_AUTH_TOKEN`, `SPARTASK_PLATFORM_API_TOKEN` ...). `SECRET_KEY` ve veritabanı şifreleri sadece ilk kurulumda üretilir; sonradan boşalırsa yedekten geri yüklenmelidir,
  - **ZORUNLU** (boşsa sistem çalışmaz) ve **ÖNERİLEN** (boşsa ilgili özellik kapalı kalır) ayarları açıklamalarıyla listeler, geçersiz değerleri gösterir,
  - sunucuya özel öneri verir: firma listesini gösterip `SPARTASK_PLATFORM_TENANT_IDS` için doğru kimliği, DigitalOcean'da domain Cloudflare arkasındaysa `SPARTASK_EGRESS_DENY_CIDRS` değerini önerir.
  `update` ve `restart` aynı kontrolü otomatik yapar; zorunlu bir ayar eksikse hiçbir şeyi değiştirmeden durur. Rapor şifre değerlerini asla göstermez; bir değeri görmek için: `sudo grep ^SPARTASK_PLATFORM_API_TOKEN= .env`.
- `update` başarısız olursa `.env` önceki sürüme döner; `spartaskctl restart` eski sürümü tekrar ayağa kaldırır. Başarısız bir migration dosyası kendi transaction'ında geri alınır.
- Yedek içeriği: `spartask.dump` (pg_dump), `storage.tar.gz`, `config.tar.gz` (.env, sertifikalar), marketplace varsa `marketplace.dump` ve `marketplace-storage.tar.gz`. Yedekleri sunucu dışına da kopyalayın (DigitalOcean backup, S3, vb.).
- Veritabanına bağlanmak: `sudo docker compose exec db psql -U spartask spartask`

## Kayıt ve platform süreçleri

- **E-posta onaylı kayıt:** Şifreyle kayıt olan firma, onay e-postasındaki bağlantı açılınca oluşturulur (bağlantı 24 saat geçerli). Bunun için `.env` içinde `INVITATION_SMTP_*` dolu olmalıdır; SMTP yoksa production'da şifreyle kayıt kapalıdır (Google ile kayıt çalışır).
- **Platform olayları:** Yeni firma oluştuğunda `tenant.created` olayı scheduler tarafından `PLATFORM_EVENTS_WEBHOOK_URL` adresine (`X-Webhook-Token` başlığıyla) gönderilir; teslim edilemezse artan aralıklarla tekrar denenir. Önerilen hedef, kendi firmanızdaki bir **API Dinleyici** sürecidir:
  1. Kendi firmanızda (ör. `https://spartask.spartask.ai`) API Dinleyici ile başlayan bir süreç oluşturun: yol `tenant-created`, güvenlik anahtarı `openssl rand -hex 24` çıktısı. Olay verisi `body.data` altındadır (`tenant_id`, `name`, `subdomain`, `admin_email`, `admin_full_name`, `signup_method`, `created_at`).
  2. `.env`: `PLATFORM_EVENTS_WEBHOOK_URL=https://spartask.spartask.ai/api/webhooks/tenant-created`, `PLATFORM_EVENTS_WEBHOOK_TOKEN=<aynı anahtar>`, ardından `spartaskctl restart`. URL boşken olaylar `public.platform_events` tablosunda bekler ve ayar yapılınca teslim edilir.
- **Firmalara hatırlatıcılar:** Kendi firmanızda zamanlanmış bir süreç, firma listesini **platform API'sinden** alır (`GET /api/platform/tenants`). API yalnızca `SPARTASK_PLATFORM_TENANT_IDS` içindeki firmanın adresinde çalışır; başka her firmaya `401` döner.
  1. `sudo ./spartaskctl config`: sunucudaki firmaları listeler ve `SPARTASK_PLATFORM_API_TOKEN`'ı üretir. Listeden kendi firmanızın `id`'sini `.env` içinde `SPARTASK_PLATFORM_TENANT_IDS=` satırına yazın, ardından `spartaskctl restart`. Token'ı görmek için: `sudo grep ^SPARTASK_PLATFORM_API_TOKEN= .env`.
  2. Süreçte **HTTP İstemcisi (WEBHOOK_SENDER)** düğümü: `GET https://<firmanız>.<domain>/api/platform/tenants?status=active`, başlık `X-Spartask-Platform-Token: <token>`. Yanıt `response_body.tenants` altında: `id`, `name`, `subdomain`, `status`, `contact_email`, `created_at`, `credit_balance`. Filtreler: `status` (active/suspended), `created_after` / `created_before` (RFC 3339), `limit` (en çok 500), `offset`.
  3. Web oturumuyla çağrı da mümkündür (sadece o firmanın ADMIN kullanıcıları).
- **Platform veritabanı:** Firma veri kaynakları platform servislerine (`db`, `nats`, marketplace) bağlanamaz. Önceki sürümlerde önerilen `platform_reader` / `host=db` veri kaynağı artık reddedilir; bu kullanıcıyı silebilirsiniz: `DROP ROLE IF EXISTS platform_reader;`

## Firma bağlantılarında ağ güvenliği

Veri kaynakları (PostgreSQL, MySQL, SQL Server, modül migration'ları dahil), mail sunucuları, yapay zeka uç noktaları, HTTP İstemcisi düğümü, MCP sunucuları ve Excel indirmeleri aynı politikayla açılır:

- Loopback, link-local (bulut metadata servisi `169.254.169.254`), çok noktaya yayın ve özel amaçlı IP aralıkları **her zaman** reddedilir. Stack'in kendi ağları (`SPARTASK_SUBNET`, marketplace alt ağları) da her zaman reddedilir.
- Özel ağlar (10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7) varsayılan olarak kapalıdır:
  - **Tek firmalık on-premise kurulum:** `SPARTASK_EGRESS_ALLOW_PRIVATE=true` ile tüm firmalar şirket ağına erişir.
  - **Firma bazlı istisna:** `SPARTASK_EGRESS_PRIVATE_GRANTS='[{"tenant":"tenant_acme","host":"db.musteri.local","port":5432,"cidrs":["10.80.4.20/32"]}]'` (`tenant` = firma şema adı; host, port ve IP birlikte eşleşmelidir).
- Ad çözümlemesi bağlantı anında bir kez yapılır, dönen adreslerin hepsi kontrol edilir ve bağlantı kontrol edilen adrese açılır (DNS rebinding işe yaramaz). HTTP yönlendirmeleri de kontrol edilir; ortam proxy'leri kullanılmaz.
- Sunucunun public IP'sini `SPARTASK_EGRESS_DENY_CIDRS`'e ekleyin — **yalnızca domain Cloudflare proxy'si arkasındaysa.** Aksi halde domain'in adresi de bu IP'dir ve firmaların kendi Spartask API'nize (ör. platform API'si) yaptığı çağrılar da engellenir.
- Yerel (stdio) MCP sunucuları kaldırıldı: firma süreçleri sunucuda komut çalıştıramaz. Bu ayarı kullanan süreçler hata verir; HTTP MCP sunucusuna taşınmalıdır.
- Engellenen hedef API'de `403 OUTBOUND_TARGET_DENIED` olarak döner.
- NATS, `NATS_AUTH_TOKEN` ile korunur. `install` üretir; eski kurulumlarda `config`/`update`/`restart` eksik token'ı `.env`'e ekler (mevcut değer değişmez). Düz `docker compose` kullanıyorsanız önce `NATS_AUTH_TOKEN=$(openssl rand -hex 32)` ekleyin.
- Marketplace veritabanı yalnızca marketplace backend'iyle aynı internal ağdadır; engine (api/worker) marketplace ağlarına bağlı değildir.

**Yükseltmeden önce:** iç ağdaki veritabanı/SMTP/Ollama/MCP hedeflerini kullanan süreçleri belirleyin ve yukarıdaki ayarlardan uygun olanı `.env`'e ekleyin; yeni paketi açıp `spartaskctl update <sürüm>` çalıştırın.

## Sürüm çıkarma (geliştirici)

1. Lokalde geliştir (`spartask_backend` içinde `docker compose up` veya `go run`); veritabanı değişikliği yeni bir `migrations/0NN_*.sql` dosyasıdır (tekrar çalıştırılabilir yazılır).
2. `spartask_backend`'de etiket at: `git tag v1.3.0 && git push origin v1.3.0` → Actions testleri çalıştırır ve `ghcr.io/spartaskai/spartask:1.3.0` imajını yayınlar (web arayüzü `spartask_web` main dalından gömülür).
3. Marketplace için aynısı `spartask_marketplace`'te → `marketplace-api` ve `marketplace-web` imajları.
4. Sunucuda: `sudo ./spartaskctl update 1.3.0`.

Bu depo (`spartask-deploy`) değiştiğinde `git tag v1.x.y && git push origin v1.x.y` ile yeni kurulum paketi yayınlanır. Mevcut bir sunucuda compose/Caddy dosyalarını güncellemek için yeni paketi aynı klasöre açın (`.env`, `data/`, `certs/`, `backups/` dokunulmadan kalır) ve `spartaskctl config` ile yeni ayarları kontrol edip `spartaskctl restart` çalıştırın.

**Yeni bir ortam değişkeni eklerken (geliştirici):** `spartask-deploy/.env.example` içine açıklamasıyla ve `#@` kuralıyla ekleyin (kuralların anlamı dosyanın başında). `spartask_backend`'deki `internal/envcheck` testi, kodun okuyup şablonda olmayan her değişkende hata verir; böylece sunucudaki `.env` ile proje ayrışmaz.

## Sorun giderme

| Belirti | Kontrol |
|---|---|
| `install`/`update` imaj çekemiyor | `docker login ghcr.io` (private imaj), sürüm etiketinin GHCR'da olduğundan emin olun |
| API açılmıyor | `spartaskctl logs migrate` ve `spartaskctl logs api` — `SECRET_KEY`/`WEB_AUTH_JWT_SECRET` eksikse API başlamaz |
| Tarayıcıda sertifika hatası | `certs/cert.pem` domain ve `*.domain`'i kapsamalı; Cloudflare kullanılıyorsa SSL modu **Full (strict)** |
| Firma adresi açılmıyor | DNS'te `*` kaydı var mı, `SPARTASK_DOMAIN` doğru mu |
| Giden mail gitmiyor | DigitalOcean 25, 465 ve 587 portlarını engeller; mail servisinin 2525 portunu kullanın (ör. Brevo smtp-relay.brevo.com:2525) |
