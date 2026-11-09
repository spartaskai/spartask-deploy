# Spartask Deploy

Spartask'ı bir Linux sunucuya (bulut ya da müşterinin kendi sunucusu / on-premise) ya da Hyper-V ile Windows Server'a kurmak ve işletmek için gereken her şey:

| Dosya | Görevi |
|---|---|
| `compose.yml` | Uygulama yığını: Caddy (HTTPS), PostgreSQL, NATS, `migrate`, API (web arayüzü dahil), worker, scheduler |
| `compose.marketplace.yml` | Marketplace (sadece platform sunucusunda, `--with-marketplace`) |
| `caddy/` | Reverse proxy ve TLS ayarı — `domain` ve tüm firma alt alan adları (`*.domain`) |
| `.env.example`, `marketplace.env.example` | Ayar şablonları (kurulumda `.env` / `marketplace.env` olarak, mode 600, üretilmiş şifrelerle yazılır) |
| `spartaskctl` | Kurulum ve işletim aracı (Go): `install`, `update`, `backup`, `restore`, `status`, `logs`, `restart` |
| `install.sh` | Tek komutla kurulum (`get.spartask.ai`): Docker'ı kurar, paketi indirip doğrular, `spartaskctl install`'u çalıştırır |
| `install.ps1` | Windows Server'da tek komutla kurulum (`get.spartask.ai/windows`): paketi indirip doğrular, `spartaskctl hyperv install` ile Hyper-V sanal makinesine kurar |

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

**Gereksinimler:** x86_64 Linux sunucu; denenen sistem Ubuntu 24.04 LTS. Pilot başlangıç boyutu 2 vCPU / 4 GB bellek ve en az 10 GB boş disk (Hyper-V sanal makinesi varsayılan 64 GB); kullanıcı ve süreç sayısı büyüdükçe ölçüp artırın, bu bir kapasite sözü değildir. Docker Engine + Compose eklentisi (yoksa `install.sh` kurar), domain için DNS kaydı; firmalar `<firma>.<domain>` adresinde açılacaksa (spartask.ai) `*` (wildcard) kaydı da.

### Müşteri kurulumu (aktivasyon kodu)

Kendi sunucusuna SparTask kuran müşteri önce `https://accounts.spartask.ai/onprem` sayfasında SparTask hesabıyla girer, kurulumun adresini (ör. `https://spartask.firma.com.tr`) yazar ve on-prem lisans sözleşmesini onaylar. Sayfa 24 saat geçerli bir aktivasyon kodu ve tek satırlık kurulum komutunu gösterir:

```bash
curl -fsSL https://get.spartask.ai | sudo sh -s -- -activation <kod> -license-server https://accounts.spartask.ai
```

`get.spartask.ai` bu deponun son release'indeki `install.sh`'tır (`.../releases/latest/download/install.sh`). Betik:

- yalnızca Linux ve x86_64 (amd64) sunucuda çalışır, root ister,
- Docker yoksa `https://get.docker.com` ile kurar,
- son release'in `spartask-deploy-linux-amd64.tar.gz` paketini indirir, release'in `SHA256SUMS`'u ile doğrular, `/opt/spartask`'a açar (`SPARTASK_DIR` ile başka klasör; `SPARTASK_DEPLOY_VERSION=v1.2.0` ile belirli sürüm),
- `/opt/spartask`'ta yarım kalmış bir kurulum varsa (`install-state.json`) yeniden indirmeden onu sürdürür; kurulum bitmişse hiçbir şeye dokunmadan durur (kurulu sunucu `./spartaskctl update` ile güncellenir),
- `--`'dan sonraki her şeyi `spartaskctl install`'a verir; sorular terminalden okunur.

Docker sunucuda zaten kuruluysa betik ona dokunmaz. Kurumsal sunucularda Docker'ı kurumun kendi paket deposundan ya da Docker'ın resmi apt deposundan, sürüm politikanızla kurmanızı öneririz; `get.docker.com` hızlı başlangıç içindir.

Betiği önce okumak isteyen: `curl -fsSL https://get.spartask.ai -o install.sh && less install.sh && sudo sh install.sh -activation <kod> -license-server https://accounts.spartask.ai`. Elle kurulum da aynıdır: paketi indirip açın ve `sudo ./spartaskctl install -activation <kod> -license-server https://accounts.spartask.ai` çalıştırın.

`install` sürümü, yöneticinin e-postasını, adını ve şifresini ve sertifikanın nereden geleceğini sorar:

| Kullanıcılar adrese nereden ulaşır? | Sertifika | Gerekenler |
|---|---|---|
| İnternetten | `-tls acme` (varsayılan): Caddy Let's Encrypt'ten alır ve yeniler | Adresin DNS `A` kaydı sunucunun internet adresini gösterir; 80 ve 443 internetten açıktır |
| Yalnız şirket ağından ya da VPN'den | `-tls files -cert <dosya> -key <dosya>`: şirketin sertifikası (tam zincir, PEM) | Şirket DNS'i adresi sunucuya çözer; sertifika adresi kapsar. Lisans servisine ve imaj adreslerine giden HTTPS açıktır |

Sonra sırayla:

1. **Ön kontrol (hiçbir şey değiştirmez):** bellek (en az 4 GB), kurulum klasöründe ve Docker'ın veri klasöründe boş disk (en az 10 GB), 80 ve 443 portlarının boş olduğu, sunucunun ulaşması gereken adresler (aşağıda) ve sunucu saati. Sorunların hepsini birden, ne yapılacağıyla yazar (ör. "port 443: another program uses it … stop that program or install SparTask on a separate server"); bir sorun varsa hiçbir şey yazmadan ve aktivasyon kodunu kullanmadan durur.
2. Kurulum anahtarını üretir ve `.env`'i, kurulumun durumunu tutan `install-state.json` ile birlikte kaydeder (ikisi de mode 600; yönetici şifresi hiçbir dosyaya yazılmaz).
3. Kodu bu anahtarla imzalayıp lisans servisinden lisansı alır; adres ve firma adı sayfada girilenlerdir. Tek adresli `.env`: `SPARTASK_SITES` yalnızca o adres, `SPARTASK_ROOT_TENANT` firmanın adresi (firma kök adreste açılır), `SPARTASK_TLS` yöneticinin e-postası (Let's Encrypt) ya da `/certs/cert.pem /certs/key.pem`, `SPARTASK_SIGNUP_MODE=closed`. Lisans servisi bir marketplace bildiriyorsa kurulumu ona bağlar (`SPARTASK_MARKETPLACE_URL`, `MARKETPLACE_API_URL`, `MARKETPLACE_ALLOWED_ORIGINS`; `-marketplace-url` başka birini seçer) ve marketplace'in firmayı tanıdığı kimliği yazar (`SPARTASK_MARKETPLACE_COMPANY=onprem.<kurulum kimliği>`).
4. Adresi denetler: sertifika dosyaları seçildiyse sertifikanın anahtarıyla eşleştiğini, şu an geçerli olduğunu ve adresi kapsadığını; Let's Encrypt seçildiyse adresin DNS kaydı olduğunu.
5. Yığını başlatır, lisansı yükler, firmayı ve yöneticisini açar, günlük yedek görevini kurar ve `install-state.json`'ı siler.

**Yarım kalan kurulum:** 2. adımdan sonraki bir hata (ağ kesildi, imaj inmedi, DNS kaydı eksik, sertifika yanlış) düzeltilip **aynı komut** tekrar çalıştırılır; kurulum kaydedilen ayarlarla kaldığı yerden devam eder. Lisans alındıysa yeniden aktivasyon yapılmaz; alınmadıysa kod aynı kurulum anahtarıyla yeniden kullanılır. Kodun süresi dolduysa sayfadaki **Yeni kod** ile alınan kod `-activation` ile verilir, kurulum yine aynı anahtarla sürer. Firma henüz açılmadıysa yönetici şifresi yeniden sorulur.

Kurulumdan sonra firmanın yöneticisi stüdyodaki **Marketplace** bağlantısıyla SparTask hesabına girer ve ücretsiz executor'lu modülleri (ör. CRM) kurar; executor lisansları kuruluma bağlıdır ve scheduler onları 6 saatte bir yeniler. Ücretli executor'lar şimdilik yalnız spartask.ai'de kullanılabilir.

Etkileşimsiz kurulumda (`-y`) yönetici bilgileri `-admin-email`, `-admin-name` bayraklarıyla, şifre `SPARTASK_ADMIN_PASSWORD` ortam değişkeniyle verilir; günlük yedek görevi istenmiyorsa `-backup-job=false`. Firma adından adres kimliği üretilemezse `-company-id` ile verilir.

#### Sunucunun ulaştığı adresler

Kullanıcıların erişimi dışında sunucu yalnız dışarıya bağlantı kurar; içeriye yalnız 80/443 (Let's Encrypt'te internetten, sertifika dosyalarında şirket ağından) gerekir.

| Adres | Ne zaman | Ne için |
|---|---|---|
| `ghcr.io`, `pkg-containers.githubusercontent.com` | kurulum ve güncelleme | SparTask imajı |
| `registry-1.docker.io` ve Docker Hub'ın indirme adresleri | kurulum ve güncelleme | Caddy, PostgreSQL, NATS imajları |
| `github.com`, `objects.githubusercontent.com` | kurulum ve güncelleme | `install.sh` ve `spartask-deploy` paketi |
| `accounts.spartask.ai` | aktivasyonda; sonra periyodik (6 saatte bir kontrol, bitime 30 gün kala yenileme) | kurulum ve executor lisansları |
| `marketplace.spartask.ai` | modül ya da executor kurulurken | paket indirme |
| `acme-v02.api.letsencrypt.org` | yalnız Let's Encrypt seçildiyse, sertifika alınırken ve yenilenirken | sertifika |
| `cloud-images.ubuntu.com` | yalnız Windows (Hyper-V) kurulumunda bir kez | Ubuntu imajı |
| Ubuntu ve Docker paket depoları | sistem güncellemelerinde | işletim sistemi |
| DNS, NTP (saat), SMTP | sürekli / gerektikçe | ad çözme, saat senkronizasyonu, e-posta (`INVITATION_SMTP_*`) |
| Firmaların kendi entegrasyonları | süreçler çalışırken | veri kaynakları, yapay zeka sağlayıcıları, webhook'lar |

Lisans servisine bağlantı sürekli gerekmez: ulaşılamazsa lisans süresi boyunca ve bitiminden sonra 30 gün daha her şey çalışır; sonra yeni firma açılamaz ve marketplace kapanır, çalışan süreçler sürer. Proxy arkasındaki sunucuda `HTTPS_PROXY` hem kurulum komutuna (`curl -fsSL https://get.spartask.ai | sudo HTTPS_PROXY=http://proxy:3128 sh -s -- ...`; `sudo` ortamı aktarmaz) hem Docker'ın kendi proxy ayarına verilmelidir; proxy sertifikaları değiştiriyorsa kurumun kök sertifikası sisteme eklenir (ön kontrol bunu ayrıca söyler).

#### Teslim

Kurulumu müşteriye bırakmadan önce:

- Adres tarayıcıda açılıyor, yönetici giriyor, CRM modülü kuruluyor ve bir süreç baştan sona çalışıyor.
- `.env`'in kopyası (`SECRET_KEY`, `SPARTASK_INSTALLATION_KEY`, `BACKUP_ENCRYPTION_KEY`) müşterinin şifre yöneticisinde.
- Günlük yedek görevi kurulu (`/etc/cron.d/spartask-backup`); yedeklerin sunucu dışına nereye kopyalanacağı ve geri yüklemenin ne zaman denenmesi gerektiği müşteriyle belirlendi. Aynı diskteki yedek sunucunun kaybına karşı korumaz; anahtarı yalnız o sunucuda olan yedek de açılamaz.
- Sertifika dosyaları seçildiyse sertifikanın bitiş tarihi ve yenilenince `certs/` + `spartaskctl restart` yapılacağı biliniyor.

### Windows Server'a kurulum

Windows'ta SparTask bir Hyper-V sanal makinesinde (Ubuntu 24.04) çalışır; içine yukarıdaki `install.sh` aynen kurulur. PowerShell'i **Yönetici olarak çalıştır** ile açıp sayfadaki Windows komutunu çalıştırın:

```powershell
& ([scriptblock]::Create((irm https://get.spartask.ai/windows))) -activation <kod> -license-server https://accounts.spartask.ai
```

`get.spartask.ai/windows` bu deponun son release'indeki `install.ps1`'dir (`.../releases/latest/download/install.ps1`). Betik `spartask-deploy-windows-amd64.zip`'i indirir, `SHA256SUMS` ile doğrular, `C:\ProgramData\SparTask`'a açar ve `spartaskctl hyperv install`'u verilen bayraklarla çalıştırır (`$env:SPARTASK_DEPLOY_VERSION = 'v1.2.0'` belirli sürüm). `spartaskctl hyperv install` sırayla:

1. Yönetici yetkisini, Hyper-V'yi ve OpenSSH istemcisini (`ssh.exe`) denetler. Hyper-V kapalıysa açar (Windows Server'da rol, Windows 10/11 Pro'da özellik) ve durur: Windows'u yeniden başlatıp **aynı komutu** tekrar çalıştırırsınız.
2. `SparTask` adlı iç sanal anahtarı ve NAT ağını kurar (varsayılan `172.30.230.0/24`: bilgisayar `.1`, sanal makine `.10`), bilgisayarın 80 ve 443 TCP portlarını sanal makineye yönlendirir ve Windows Güvenlik Duvarı'nda bu portları açar. Ağ bilgisayarın bir adresiyle ya da başka bir NAT ağıyla (WSL, Docker Desktop) çakışıyorsa veya 80/443'ü başka bir program (IIS) kullanıyorsa hiçbir şey kurmadan nedenini söyler.
3. Ubuntu 24.04 bulut imajını `cloud-images.ubuntu.com`'dan indirir, `SHA256SUMS` ile doğrular ve kendisi (ek program olmadan) Hyper-V diskine çevirir; cloud-init ayarını (sabit adres, DNS, `spartask` kullanıcısı ve bu bilgisayarın SSH anahtarı) ikinci bir diske yazar.
4. 2. nesil sanal makineyi açar (Secure Boot, sabit bellek, otomatik checkpoint kapalı; Windows açılınca açılır, kapanınca düzgünce kapanır) ve başlatır.
5. SSH ile bağlanıp ilk açılışın bitmesini bekler ve sanal makinede aynı sürümün `install.sh`'ını `--`'dan sonraki bayraklarla çalıştırır; sorular bu PowerShell penceresinden yanıtlanır, yönetici şifresi hiçbir dosyaya yazılmaz.

Sanal makinenin donanımı ilk kurulumda `-cpus 2 -memory-gb 4 -disk-gb 64` bayraklarıyla, ağı `-subnet` ile, DNS sunucuları `-dns 10.0.0.2,10.0.0.3` ile seçilir (varsayılan: bilgisayarın kendi DNS sunucuları; bilgisayar DNS'i kendisi veriyorsa `-dns` gerekir). Bu bayraklar komutun herhangi bir yerine yazılabilir, gerisi `spartaskctl install`'a gider. Komut her adımda yarım kalanı tamamlar: yeniden başlatmadan, bir hatadan ya da kesilen bağlantıdan sonra aynı komut kaldığı yerden devam eder; kurulum bitmişse yalnızca yönetim komutlarını gösterir.

Gereksinimler: Windows Server 2019 ya da sonrası (Windows 10/11 Pro da olur), x86_64, işlemcide sanallaştırma açık (sanal sunucuda sağlayıcının iç içe sanallaştırmaya izin vermesi gerekir), sanal makine için 4 GB bellek ve 64 GB disk. Let's Encrypt seçildiyse adresin DNS `A` kaydı bu bilgisayarın internet adresini göstermeli, 80 ve 443 internetten açık olmalıdır (bulut sağlayıcının güvenlik duvarı dahil); şirket ağı kurulumunda şirket DNS'i adresi bu bilgisayarın ağ adresine çözer ve sertifika dosyaları bu bilgisayardaki yollarıyla verilir: `... -tls files -cert C:\certs\spartask.pem -key C:\certs\spartask.key`; komut onları sanal makineye kopyalar (kurulum bitince kopyalar silinir, kurulum `certs/`'te kendi kopyasını tutar). Bilgisayar kendi internet adresiyle sanal makineye ulaşamaz (NAT); sitenin kontrolü başka bir cihazdan yapılır.

Dosyalar `C:\ProgramData\SparTask` altındadır: `spartask-deploy\` (araç), `vm\` (sanal makine ve diskleri), `ssh\` (yalnız Administrators ve SYSTEM'in okuyabildiği SSH anahtarı). Kurulum sanal makinenin içinde `/opt/spartask`'tadır ve oradaki `spartaskctl` ile işletilir:

```powershell
C:\ProgramData\SparTask\spartask-deploy\spartaskctl.exe hyperv ssh -- sudo /opt/spartask/spartaskctl -dir /opt/spartask status
C:\ProgramData\SparTask\spartask-deploy\spartaskctl.exe hyperv ssh      # sanal makinede kabuk
```

Yedekler sanal makinenin `/opt/spartask/backups/` klasörüne yazılır; Hyper-V'nin kendi yedeği (sanal makinenin dışa aktarımı) bunun yerine geçmez, tutarlı yedek `spartaskctl backup`'tır.

### Platform ve diğer kurulumlar

```bash
sudo mkdir -p /opt/spartask && cd /opt/spartask
curl -fsSL https://github.com/spartaskai/spartask-deploy/releases/latest/download/spartask-deploy-linux-amd64.tar.gz | sudo tar xz --strip-components=1

# TLS sertifikası: domain ve *.domain'i kapsayan sertifika + anahtar (ör. Cloudflare Origin Certificate)
sudo mkdir -m 700 certs
sudo nano certs/cert.pem     # "Origin Certificate" içeriği
sudo nano certs/key.pem      # "Private Key" içeriği

sudo ./spartaskctl install
```

`install` şunları sorar: domain, sürüm, TLS modu, Cloudflare kullanımı ve günlük yedek görevi. Tüm şifreleri kendisi üretir, imajları çeker, migration'ları uygular ve API sağlıklı olana kadar bekler.

Örnekler:

```bash
# Platform sunucusu (spartask.ai, Cloudflare arkasında, marketplace dahil; marketplace imajları private)
# -marketplace-signing-key: şifre yöneticisindeki ED25519_PRIVATE_KEY'i içeren dosya (yeni anahtar üretilmez)
sudo ./spartaskctl install -domain spartask.ai -cloudflare -with-marketplace -marketplace-signing-key /root/marketplace.key \
     -registry-user <github-kullanıcısı>

# Kapalı ağ (sertifika yok, Caddy kendi CA'sını kullanır)
sudo ./spartaskctl install -domain spartask.local -tls internal
```

> **Önemli:** `.env` içindeki `SECRET_KEY`, veri kaynağı / mail sunucusu / yapay zeka şifrelerini; `BACKUP_ENCRYPTION_KEY` ise yedekleri şifreler. Kurulumdan sonra `.env`'in bir kopyasını şifre yöneticisine koyun; bu anahtarlar olmadan yedekler açılamaz.

SparTask imajı (`ghcr.io/spartaskai/spartask`) public'tir; giriş gerekmez. Marketplace imajları (yalnızca platform sunucusunda) private'tır: `-registry-user` ile `read:packages` yetkili bir GitHub token'ı sorulur (veya önceden `docker login ghcr.io`).

## Günlük işletim

```bash
sudo ./spartaskctl config              # .env'i bu paketin şablonuyla karşılaştır: eksikleri ekle, şifreleri üret, doldurulacakları açıkla
sudo ./spartaskctl config -check       # aynı rapor, dosyaya dokunmadan
sudo ./spartaskctl status              # sürüm, container'lar, migration durumu
sudo ./spartaskctl update 1.3.0        # imajı indir (eski sürüm çalışırken) → yedek al → migrate → yeniden başlat → sağlık kontrolü
sudo ./spartaskctl update 1.3.0 -marketplace-version 1.3.0
sudo ./spartaskctl logs api            # canlı loglar (api, worker, scheduler, migrate, caddy, db ...)
sudo ./spartaskctl restart             # .env değişikliklerini uygula
sudo ./spartaskctl backup              # backups/<zaman>/ altına yedek (en yeni BACKUP_KEEP adet tutulur)
sudo ./spartaskctl demo apply demo/demo-mobilya.json  # demo şirketini kur veya sıfırla (aşağıda)
sudo ./spartaskctl system-processes sync  # sistem süreçlerini (Çalışanlar, Format Profilleri) tüm şirketlerde şimdi güncelle
sudo ./spartaskctl restore backups/20261004T033000Z               # veriyi yedekten geri yükle
sudo ./spartaskctl restore backups/20261004T033000Z -with-config  # yeni sunucuya taşıma: .env ve sertifikalar da
# yeni sunucuda yedeğin anahtarı henüz .env'de yoksa şifre yöneticisinden verin (verilmezse sorulur):
sudo BACKUP_ENCRYPTION_KEY=... ./spartaskctl restore backups/20261004T033000Z -with-config
```

- **Ayarlar (`.env`):** Her ayarın açıklaması ve kuralı `.env.example` şablonundadır. `spartaskctl config` sunucudaki `.env`'i şablonla karşılaştırır:
  - yeni sürümle gelen ayarları açıklamalarıyla birlikte doğru bölüme ekler (mevcut değerlere dokunmaz, önceki dosyayı `.env.bak` olarak saklar),
  - güvenle üretilebilen şifreleri üretir (`NATS_AUTH_TOKEN`, `SPARTASK_PLATFORM_API_TOKEN` ...). `SECRET_KEY` ve veritabanı şifreleri sadece ilk kurulumda üretilir; sonradan boşalırsa yedekten geri yüklenmelidir,
  - **ZORUNLU** (boşsa sistem çalışmaz) ve **ÖNERİLEN** (boşsa ilgili özellik kapalı kalır) ayarları açıklamalarıyla listeler, geçersiz değerleri gösterir,
  - sunucuya özel öneri verir: firma listesini gösterip `SPARTASK_PLATFORM_TENANT_IDS` için doğru kimliği, DigitalOcean'da domain Cloudflare arkasındaysa `SPARTASK_EGRESS_DENY_CIDRS` değerini önerir.
  `update` ve `restart` aynı kontrolü otomatik yapar; zorunlu bir ayar eksikse hiçbir şeyi değiştirmeden durur. Rapor şifre değerlerini asla göstermez; bir değeri görmek için: `sudo grep ^SPARTASK_PLATFORM_API_TOKEN= .env`.
- `update` önce yeni imajları indirir; inemezse hiçbir şey değişmez ve eski sürüm çalışmayı sürdürür. Yeni sürüm açılmazsa `.env` önceki sürüme döner ve `spartaskctl restart` eski sürümü başlatır. **Otomatik veritabanı geri dönüşü yoktur:** başarısız migration dosyası kendi transaction'ında geri alınır, ama aynı güncellemede başarıyla uygulanmış migration'lar veritabanında kalır ve eski sürüm bu şemayla açılmayabilir. O durumda hata mesajındaki güncelleme öncesi yedek `spartaskctl restore` ile geri yüklenir; yedekten sonra yazılan veri kaybolacağı için bu karar bilerek verilir.
- Yedek içeriği: `spartask.dump` (pg_dump), `storage.tar.gz`, `config.tar.gz` (.env, sertifikalar), marketplace varsa `marketplace.dump` ve `marketplace-storage.tar.gz`. Dosyalar `BACKUP_ENCRYPTION_KEY` ile şifrelenir (AES-256-GCM) ve `.enc` uzantısı alır; anahtar `config`/`update` ile üretilir. Anahtar boşsa yedek şifresiz alınır ve uyarı verilir. Şifreli yedekleri sunucu dışına da kopyalayın (barındırma firmasının yedek alanı, S3 uyumlu depolama vb.; kişisel veri içerdiği için mümkünse Türkiye'de); eski şifresiz yedekler de geri yüklenebilir.
- Veritabanına bağlanmak: `sudo docker compose exec db sh -c 'psql -U "$POSTGRES_USER" "$POSTGRES_DB"'`, ya da kendi bilgisayarından SSH tüneliyle (aşağıda).

## Veritabanına bağlanmak

PostgreSQL dışarıya açık değildir; yalnızca sunucunun kendi içinde `127.0.0.1:<DB_LOCAL_PORT>`'ta (varsayılan `5433`)
dinler. Kendi bilgisayarından SSH tüneliyle bağlanılır (ör. DBeaver). Veriler firmaların süreçleri ve uygulama
üzerinden değişir; elle yazma tutarlılığı bozabilir. Bu yüzden salt okunur bir kullanıcı kullanılır.

**Salt okunur kullanıcı** (bir kez). `pg_read_all_data` bütün şemaları (firma şemaları dahil, sonradan açılanlar da)
okuyabilir, hiçbir şey yazamaz. Komut şifreyi bir kez gösterir; şifre yöneticisine kaydedin:

```bash
cd /opt/spartask
RO=<salt okunur kullanıcının adı>
PW=$(openssl rand -hex 24); echo "$RO şifresi: $PW"
sudo docker compose exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" "$POSTGRES_DB"' <<SQL
CREATE ROLE "$RO" LOGIN PASSWORD '$PW';
GRANT pg_read_all_data TO "$RO";
SQL
unset PW RO
```

**DBeaver:** Yeni bağlantı → PostgreSQL. **Main:** Host `localhost`, Port `<DB_LOCAL_PORT>`, Database `.env`'deki
`DB_NAME`, Username yukarıda açılan salt okunur kullanıcı. **SSH** sekmesi: Use SSH Tunnel; sunucunun IP'si, kullanıcı ve anahtar. Ed25519 anahtarı
okunamazsa SSH sekmesinin gelişmiş ayarlarında Implementation `SSHJ` seçilir.

## Demo şirketi

Tanıtım, Pioneer görüşmeleri ve mağaza incelemeleri için demo şirketler tek bir JSON dosyasından kurulur. `spartaskctl demo apply` dosyadaki şirketi, rolleri, kullanıcıları ve veri kaynaklarını oluşturur; şirket zaten varsa onu dosyadaki hale getirir: şifreler sıfırlanır, eski oturumlar kapanır, kullanıcıların rolleri tam olarak dosyadakiler olur, ADMIN olmayanların 2FA'sı kaldırılır (ADMIN için 2FA zorunludur, telefonundaki kalır), veri kaynakları adıyla eşleştirilip güncellenir. Dosyada olmayan kullanıcılara dokunulmaz, raporda listelenir. Modüller (ör. CRM) ve örnek verileri bu dosyanın parçası değildir: şirkete müşterinin yaptığı gibi marketplace'ten kurulur, örnek veri modülün kendi "Örnek veriyi yükle" süreciyle yüklenir.

```bash
cp demo/demo-mobilya.example.json demo/demo-mobilya.json   # e-postaları ve veritabanını düzenleyin
sudo ./spartaskctl demo check demo/demo-mobilya.json       # doğrula, hiçbir şey yazma
sudo ./spartaskctl demo apply demo/demo-mobilya.json
```

- **Güvenlik:** Yalnızca adresi `demo-` ile başlayan şirketler oluşturulur veya değiştirilir; `SPARTASK_PLATFORM_TENANT_IDS` içindeki şirketler reddedilir. Gerçek bir müşteri şirketi bu komutla değiştirilemez.
- **Şifreler dosyada durmaz:** `"env:AD"` yazılan değerler önce spartaskctl'in ortamından, yoksa `.env`'den okunur. Bu adları demo dosyası belirler, `.env.example` şablonunda yer almazlar; bu yüzden `spartaskctl config` onları üretmez ve "şablonda olmayan ayarlar" olarak listeler (beklenen durum).
  - **Kullanıcı şifreleri** (`users[].password`, ör. `DEMO_ADMIN_PASSWORD`, `DEMO_PASSWORD`) `.env`'de yoksa `demo apply` rastgele üretip `.env`'in sonuna ekler (önceki dosya `.env.bak`), sonraki çalıştırmalar aynı değeri kullanır. `demo check` hiçbir şey yazmaz, yalnızca üretileceğini söyler. Görmek için: `sudo grep ^DEMO_ .env`. Kendiniz belirlemek isterseniz önceden `.env`'e yazın (en az 12 karakter).
  - **Veri kaynağı değerleri** (`DEMO_DB_HOST`, `DEMO_DB_PASSWORD`) dışarıdaki bir veritabanına aittir, üretilemez: `.env`'e elle yazılır, eksikse komut hiçbir şeyi değiştirmeden durur. Kullanıcı şifresiyle aynı değişken bir veri kaynağında da kullanılıyorsa o değişken de üretilmez.
- **Yeni şirket** normal kayıt servisiyle açılır (şema, migration'lar, ilk ADMIN, `tenant.created` olayı); e-posta doğrulaması ve kayıt daveti gerekmez. Dosyadaki ilk ADMIN kullanıcı şirketi açan yöneticidir.
- **Veri kaynağı:** Bağlantı kurulumda test edilmez. Uzak bir PostgreSQL için `ssl_mode: require` kullanın; sunucuda SSL yoksa `disable` yazın.
- **Her gece sıfırlama** (ör. Google Play inceleme hesabı). Önce şirket ve kullanıcılar, ardından (şirket içinde zamanlanmış) örnek veri süreci çalışır:

  ```cron
  45 2 * * * cd /opt/spartask && ./spartaskctl demo apply demo/play-review.json >> /var/log/spartask-demo.log 2>&1
  ```

- Komut api container'ı içindeki `/app/bin/spartask-admin` ile çalışır; container çalışır durumda olmalıdır.

## Kurulum lisansı

Yeni firma açmak için kurulumun bir **kurulum lisansı** olmalıdır; spartask.ai dahil. Lisans, lisans servisinin (accounts.spartask.ai) bu kurulumun anahtarı için verdiği bir dosyadır. Lisans yoksa mevcut firmalar çalışmaya devam eder, yalnızca yeni firma açılamaz.

```bash
sudo ./spartaskctl license show                 # kurulum anahtarının parmak izi (sha256:...) ve lisans durumu
sudo ./spartaskctl license install license.jws  # lisans servisinin verdiği lisansı doğrular ve yükler
sudo ./spartaskctl license renew                # lisansı lisans servisinden şimdi yeniler
```

- `install -activation` lisansı kendisi alır ve yükler (yukarıda "Müşteri kurulumu"); elle yükleme spartask.ai ve aktivasyon kullanılamayan kurulumlar içindir.
- Anahtar `.env` içindeki `SPARTASK_INSTALLATION_KEY`'dir; `install`/`config` üretir. Lisans ona bağlıdır: anahtar değişirse (`.env` kaybı) yeni lisans gerekir. `.env`'in kopyasını şifre yöneticisinde saklayın.
- Ücretsiz lisans tek firmalıktır (`community`); spartask.ai'nin lisansı sınırsızdır (`platform`).
- `SPARTASK_LICENSE_SERVER` doluysa (`install -activation` yazar) lisans bitimine 30 gün kala kendiliğinden yenilenir. On-prem lisans sözleşmesi değiştiyse yenileme bekler: platform firmasının yöneticisi uygulamada yeni sürümü onaylayınca lisans hemen yenilenir. Yenileme başarısızsa yöneticiler uygulamada uyarı görür.
- Lisansın süresi dolarsa 30 gün ek süre vardır; sonra yeni firma açılamaz, çalışan her şey sürer.

## Kayıt ve platform süreçleri

- **E-posta onaylı kayıt:** Şifreyle kayıt olan firma, onay e-postasındaki bağlantı açılınca oluşturulur (bağlantı 24 saat geçerli). Bunun için `.env` içinde `INVITATION_SMTP_*` dolu olmalıdır; SMTP yoksa production'da şifreyle kayıt kapalıdır (Google ile kayıt çalışır).
- **Davetli kayıt (başlangıç programı):** `.env` içinde `SPARTASK_SIGNUP_MODE=invite` iken yeni firma yalnızca davet bağlantısıyla açılabilir (`closed`: tamamen kapalı, `open`: herkese açık). Davet bağlantısı (`https://<domain>/register#invite=...`) tek bir e-posta adresine bağlıdır, bir kez kullanılır ve 7 gün geçerlidir; firma oluşturulunca harcanır. Google ile kayıtta da Google hesabının e-postası davetle aynı olmalıdır.
  1. Davetler `SPARTASK_PLATFORM_TENANT_IDS` içindeki firmanın ADMIN kullanıcılarına görünen **Kayıt davetleri** ekranından (`/settings/signup-invites`) oluşturulur, gönderilir ve iptal edilir. Bağlantı yalnızca oluşturulduğu anda gösterilir.
  2. Kendi süreçlerinizden: `POST https://<firmanız>.<domain>/api/platform/signup-invites`, başlık `X-Spartask-Platform-Token: <token>`, gövde `{"email":"...","program":"Başlangıç programı","note":"","send":true,"language":"tr"}`. Yanıt `link` ve `sent` döner (e-posta gönderilemezse `delivery_error: MAIL_NOT_SENT`). Liste: `GET` aynı adres; iptal: `POST .../signup-invites/<id>/revoke`.
  3. `SPARTASK_SIGNUP_CONTACT_EMAIL=info@spartask.ai` yazılırsa davetsiz gelenlere kayıt sayfasında bu adres gösterilir ("Programa katılmak için bize yazın"). Boşsa adres gösterilmez.
  4. Program bitince `SPARTASK_SIGNUP_MODE=open` yapıp `spartaskctl restart`. Davet oluşturma ve iptal işlemleri denetim kaydına (audit log) yazılır.
- **Platform olayları:** Yeni firma oluştuğunda `tenant.created` olayı scheduler tarafından `PLATFORM_EVENTS_WEBHOOK_URL` adresine (`X-Webhook-Token` başlığıyla) gönderilir; teslim edilemezse artan aralıklarla tekrar denenir. Önerilen hedef, kendi firmanızdaki bir **API Dinleyici** sürecidir:
  1. Kendi firmanızda (ör. `https://spartask.spartask.ai`) API Dinleyici ile başlayan bir süreç oluşturun: yol `tenant-created`, güvenlik anahtarı `openssl rand -hex 24` çıktısı. Olay verisi `body.data` altındadır (`tenant_id`, `name`, `subdomain`, `admin_email`, `admin_full_name`, `signup_method`, `created_at`).
  2. `.env`: `PLATFORM_EVENTS_WEBHOOK_URL=https://spartask.spartask.ai/api/webhooks/tenant-created`, `PLATFORM_EVENTS_WEBHOOK_TOKEN=<aynı anahtar>`, ardından `spartaskctl restart`. URL boşken olaylar `public.platform_events` tablosunda bekler ve ayar yapılınca teslim edilir.
- **Firmalara hatırlatıcılar:** Kendi firmanızda zamanlanmış bir süreç, firma listesini **platform API'sinden** alır (`GET /api/platform/tenants`). API yalnızca `SPARTASK_PLATFORM_TENANT_IDS` içindeki firmanın adresinde çalışır; başka her firmaya `401` döner.
  1. `sudo ./spartaskctl config`: sunucudaki firmaları listeler ve `SPARTASK_PLATFORM_API_TOKEN`'ı üretir. Listeden kendi firmanızın `id`'sini `.env` içinde `SPARTASK_PLATFORM_TENANT_IDS=` satırına yazın, ardından `spartaskctl restart`. Token'ı görmek için: `sudo grep ^SPARTASK_PLATFORM_API_TOKEN= .env`.
  2. Süreçte **HTTP İstemcisi (WEBHOOK_SENDER)** düğümü: `GET https://<firmanız>.<domain>/api/platform/tenants?status=active`, başlık `X-Spartask-Platform-Token: <token>`. Yanıt `response_body.tenants` altında: `id`, `name`, `subdomain`, `status`, `contact_email`, `created_at`. Filtreler: `status` (active/suspended), `created_after` / `created_before` (RFC 3339), `limit` (en çok 500), `offset`.
  3. Web oturumuyla çağrı da mümkündür (sadece o firmanın ADMIN kullanıcıları).
- **Günlük süreç kontrolü:** `GET /api/platform/process-health` (aynı token veya platform firmasının ADMIN oturumu) o günün (`Europe/Istanbul`; `date=YYYY-MM-DD`, `tz=`, `lang=tr|en|de`) tüm firma süreçlerini ve açık kalan süreçleri tarar; takılan/hata veren süreçleri, uzun bekleyen onayları, hata artışlarını ve worker/scheduler birikmesini döndürür (`headline`, `report_text`, `has_anomalies`, firma bazında `anomalies`). Hazır süreç: spartask_pipelines `processes/platform/process_health_check` (API bağlantısı kodu `spartask-platform`). Rapor form verisi içermez.
- **Yapay zeka:** Platform genelinde bir yapay zeka anahtarı yoktur. Her firma yapay zeka adımları ve süreç asistanı için **Altyapı > AI Provider** ekranından kendi sağlayıcısını ekleyip varsayılan yapar; firma verisi yalnızca o sağlayıcıya gider. Eski sürümlerden kalan `GEMINI_API_KEY`, `OPENAI_API_KEY` ve `GEMINI_MODEL` artık okunmaz; `.env`'den silebilirsiniz (`spartaskctl config` bunları "şablonda olmayan ayarlar" olarak listeler, API açılışta uyarı yazar).
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
| `install` ön kontrolde duruyor | Her satır ne yapılacağını söyler; düzeltip aynı komutu çalıştırın (hiçbir şey değişmemiştir) |
| `install` yarıda kaldı | Nedeni düzeltip aynı komutu çalıştırın; `install-state.json` kurulumu kaldığı yerden sürdürür |
| `install`/`update` imaj çekemiyor | Giden HTTPS (`ghcr.io`, Docker Hub; proxy varsa Docker'ın proxy ayarı), `docker login ghcr.io` (private imaj), sürüm etiketinin GHCR'da olduğu |
| API açılmıyor | `spartaskctl logs migrate` ve `spartaskctl logs api` — `SECRET_KEY`/`WEB_AUTH_JWT_SECRET` eksikse API başlamaz |
| Tarayıcıda sertifika hatası | `certs/cert.pem` tam zincir olmalı ve adresi (platform sunucusunda domain ve `*.domain`) kapsamalı; Cloudflare kullanılıyorsa SSL modu **Full (strict)** |
| Firma adresi açılmıyor | DNS'te `*` kaydı var mı, `SPARTASK_DOMAIN` doğru mu |
| Giden mail gitmiyor | Bazı sunucu sağlayıcıları 25, 465 ve 587 portlarını engeller; mail servisinin 2525 portunu kullanın (ör. Brevo smtp-relay.brevo.com:2525) |
