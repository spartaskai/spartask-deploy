# SparTask Kurulumu

SparTask'ı kendi sunucunuza kurmak ve işletmek için gereken her şey bu depodadır. Kurulum tek komuttur; komut önce sunucunuzu denetler, sonra SparTask'ı Docker ile kurar, lisansını alır ve firmanızı yöneticisiyle birlikte açar.

- [Başlamadan önce](#başlamadan-önce)
- [1. Aktivasyon kodunu alın](#1-aktivasyon-kodunu-alın)
- [2. Kurun](#2-kurun): [Linux sunucu](#linux-sunucu) · [Windows Server](#windows-server)
- [3. Kurulumu kontrol edin](#3-kurulumu-kontrol-edin)
- [4. Teslim almadan önce](#4-teslim-almadan-önce)
- [Günlük işletim](#günlük-işletim) · [Güncelleme](#güncelleme) · [Yedek ve geri yükleme](#yedek-ve-geri-yükleme)
- [Şirket ağındaki sistemlere bağlanmak](#şirket-ağındaki-sistemlere-bağlanmak) · [Lisans](#lisans) · [Sorun giderme](#sorun-giderme)

## Başlamadan önce

**1. Sunucu**

| | Linux sunucu | Windows |
|---|---|---|
| Sistem | Ubuntu 24.04 LTS, x86_64 | Windows Server 2019 ya da sonrası, Windows 10/11 Pro; x86_64, işlemcide sanallaştırma açık |
| Bellek | en az 4 GB | en az 8 GB (SparTask'ın sanal makinesine 4 GB ayrılır) |
| Disk | en az 10 GB boş, önerilen 64 GB | 64 GB boş |
| İşlemci | 2 vCPU | 2 vCPU ayrılır |

2 vCPU / 4 GB pilot için başlangıç boyutudur; kullanıcı ve süreç sayısı arttıkça ölçüp büyütün. Windows'ta SparTask bir Hyper-V sanal makinesinde (Ubuntu) çalışır; ayrı bir Windows sürümü yoktur.

**2. Adres ve sertifika**

SparTask'ın açılacağı adresi seçin, ör. `https://spartask.firmaniz.com.tr`. Kullanıcıların bu adrese nereden ulaşacağına göre:

| Kullanıcılar nereden ulaşır? | DNS kaydı | Sertifika |
|---|---|---|
| İnternetten | Adresin `A` kaydı sunucunun internet adresini gösterir; 80 ve 443 portları internetten açıktır | Kurulum Let's Encrypt'ten kendisi alır ve yeniler. Hazırlamanız gereken bir şey yok |
| Yalnızca şirket ağından ya da VPN'den | Şirket DNS'i adresi sunucunun ağ adresine çözer | Şirketinizin adresi kapsayan sertifikası ve anahtarı (PEM dosyaları; sertifika tam zincir) |

**3. Sunucunun dışarıya erişimi**

Sunucu yalnızca dışarıya bağlanır. Güvenlik duvarı ya da proxy şu adreslere giden HTTPS'e (443) izin vermelidir:

| Adres | Ne zaman | Ne için |
|---|---|---|
| `get.spartask.ai`, `github.com`, `objects.githubusercontent.com` | kurulumda | kurulum betiği ve paketi |
| `ghcr.io`, `pkg-containers.githubusercontent.com` | kurulum ve güncellemede | SparTask imajı |
| `registry-1.docker.io`, `auth.docker.io`, `production.cloudflare.docker.com` | kurulum ve güncellemede | Caddy, PostgreSQL ve NATS imajları |
| `accounts.spartask.ai` | aktivasyonda, sonra periyodik | lisans ve lisans yenileme |
| `marketplace.spartask.ai` | modül kurarken | modül ve executor paketleri |
| `acme-v02.api.letsencrypt.org` | yalnızca Let's Encrypt kullanılıyorsa | sertifika |
| `cloud-images.ubuntu.com` | yalnızca Windows kurulumunda, bir kez | Ubuntu imajı |

DNS, saat senkronizasyonu (NTP) ve e-posta (SMTP) de çalışmalıdır. Süreçleriniz kendi sistemlerinize (veritabanı, yapay zeka sağlayıcısı, web servisleri) bağlanacaksa o adresler de açık olmalıdır.

Lisans servisine sürekli bağlantı gerekmez. Ulaşılamazsa SparTask lisansın süresi boyunca ve bitiminden sonra 30 gün daha normal çalışır; sonra yeni firma açılamaz ve marketplace kapanır, çalışan süreçler sürer.

**4. SparTask hesabı**

Aktivasyon kodunu almak için bir SparTask hesabı gerekir; marketplace'e de bu hesapla girersiniz.

## 1. Aktivasyon kodunu alın

1. `https://accounts.spartask.ai/onprem` adresini açıp SparTask hesabınızla girin.
2. **Yeni kurulum** bölümünde firmanızın adını ve kurulumun adresini (ör. `https://spartask.firmaniz.com.tr`) yazın.
3. On-prem lisans sözleşmesini okuyup onaylayın ve **Kurulumu kaydet ve aktivasyon kodu al**'a basın.
4. Sayfa aktivasyon kodunu ve kurulum komutlarını gösterir. Kod 24 saat geçerlidir ve sayfadan çıkınca bir daha gösterilmez; süresi dolarsa aynı sayfadaki **Yeni kod** ile yenisini alırsınız.

## 2. Kurun

### Linux sunucu

1. Sunucuya SSH ile bağlanın.
2. Sayfadaki komutu çalıştırın:

   ```bash
   curl -fsSL https://get.spartask.ai | sudo sh -s -- -activation <kod> -license-server https://accounts.spartask.ai
   ```

   Adrese yalnızca şirket ağından ulaşılacaksa sertifika dosyalarını sunucuya kopyalayın ve komutun sonuna ekleyin:

   ```bash
   curl -fsSL https://get.spartask.ai | sudo sh -s -- -activation <kod> -license-server https://accounts.spartask.ai \
        -tls files -cert /root/spartask.pem -key /root/spartask.key
   ```

3. Soruları yanıtlayın: SparTask sürümü (Enter ile önerileni seçin), yöneticinin e-postası, adı ve şifresi (12–72 karakter, iki kez), sertifika türü, günlük yedek görevi.
4. Komut bitince SparTask'ın adresini ve yöneticiyi yazar. Adresi tarayıcıda açıp yöneticiyle girin.

Komut sırasıyla şunları yapar:

1. **Sunucuyu denetler, hiçbir şeyi değiştirmeden:** bellek, disk, 80 ve 443 portları, yukarıdaki adreslere erişim, sunucu saati. Bir sorun varsa hepsini birden ve ne yapılacağıyla yazar, aktivasyon kodunu kullanmadan durur. Sorunları giderip aynı komutu tekrar çalıştırın.
2. Docker yoksa kurar, SparTask paketini indirip doğrular ve `/opt/spartask`'a açar.
3. Ayarları (`/opt/spartask/.env`) güçlü, rastgele şifrelerle yazar.
4. Lisansı alır; adres ve firma adı kayıt sayfasında yazdıklarınızdır.
5. Sertifika dosyalarının adresi kapsadığını (ya da Let's Encrypt için adresin DNS kaydını) denetler.
6. SparTask'ı başlatır, lisansı yükler, firmanızı ve yöneticisini açar, günlük yedek görevini kurar.

**Yarıda kalırsa** (ağ kesildi, imaj inmedi, DNS kaydı ya da sertifika eksik): komutun söylediği nedeni giderip **aynı komutu** tekrar çalıştırın. Kurulum kaydettiği ayarlarla kaldığı yerden sürer. Kodun süresi dolduysa sayfadan yeni kod alın ve komuttaki kodu değiştirin. Firmanız henüz açılmadıysa yönetici şifresi yeniden sorulur.

<details>
<summary>Betiği önce okumak ya da elle kurmak isteyenler</summary>

```bash
curl -fsSL https://get.spartask.ai -o install.sh
less install.sh
sudo sh install.sh -activation <kod> -license-server https://accounts.spartask.ai
```

Belirli bir paket sürümü için: `sudo SPARTASK_DEPLOY_VERSION=v1.3.0 sh install.sh ...`. Başka bir klasöre kurmak için `SPARTASK_DIR=/srv/spartask`.

Docker sunucunuzda zaten kuruluysa betik ona dokunmaz. Kurumsal sunucularda Docker'ı kendi paket deponuzdan ya da Docker'ın resmi apt deposundan kurmanızı öneririz; betiğin kullandığı `get.docker.com` hızlı başlangıç içindir.

Proxy arkasındaki sunucuda proxy'yi komuta verin (`sudo` ortam değişkenlerini aktarmaz) ve Docker'ın kendi proxy ayarına da yazın:

```bash
curl -fsSL https://get.spartask.ai | sudo HTTPS_PROXY=http://proxy.firmaniz:3128 sh -s -- -activation <kod> -license-server https://accounts.spartask.ai
```

Etkileşimsiz kurulum: `-y` ile sorular sorulmaz; yönetici bilgileri `-admin-email` ve `-admin-name` ile, şifre `SPARTASK_ADMIN_PASSWORD` ortam değişkeniyle verilir; yedek görevi istenmiyorsa `-backup-job=false`.

</details>

### Windows Server

1. PowerShell'i **Yönetici olarak çalıştır** ile açın.
2. Sayfadaki Windows komutunu çalıştırın:

   ```powershell
   & ([scriptblock]::Create((irm https://get.spartask.ai/windows))) -activation <kod> -license-server https://accounts.spartask.ai
   ```

   Adrese yalnızca şirket ağından ulaşılacaksa sertifika dosyalarını bu bilgisayardaki yollarıyla ekleyin; komut onları sanal makineye kendisi kopyalar:

   ```powershell
   & ([scriptblock]::Create((irm https://get.spartask.ai/windows))) -activation <kod> -license-server https://accounts.spartask.ai -tls files -cert C:\certs\spartask.pem -key C:\certs\spartask.key
   ```

3. Hyper-V kapalıysa komut onu açar ve durur. Windows'u yeniden başlatın, PowerShell'i yine yönetici olarak açıp **aynı komutu** tekrar çalıştırın.
4. Sorular Linux'takiyle aynıdır ve bu pencereden yanıtlanır.
5. Adresin DNS kaydı:
   - internetten erişimde bu bilgisayarın internet adresini göstermelidir (bulut sağlayıcının güvenlik duvarında 80 ve 443 açık olmalı),
   - şirket ağında bu bilgisayarın ağ adresini göstermelidir.

Komut SparTask için `SparTask` adlı bir sanal makine ve NAT ağı (`172.30.230.0/24`) kurar, bilgisayarın 80 ve 443 portlarını sanal makineye yönlendirir ve sanal makinenin içinde Linux kurulumunu çalıştırır. Sanal makine Windows açılınca açılır, kapanınca düzgünce kapanır. Bilgisayarın 80 ya da 443 portunu başka bir program (ör. IIS) kullanıyorsa ya da ağ başka bir ağla çakışıyorsa hiçbir şey kurmadan nedenini söyler.

Seçenekler (komutun herhangi bir yerine yazılabilir): `-cpus 2`, `-memory-gb 4`, `-disk-gb 64`, `-subnet 172.30.230.0/24` (şirket ağınızla çakışırsa), `-dns 10.0.0.2,10.0.0.3` (bilgisayar DNS'i kendisi veriyorsa gerekir).

Bilgisayar kendi internet adresi üzerinden sanal makineye ulaşamaz (NAT). İnternetten erişilen kurulumu başka bir cihazdan, ör. mobil veriyle telefondan kontrol edin.

Kurulum sanal makinenin içindedir; yönetim komutları bu bilgisayardan şöyle çalışır:

```powershell
C:\ProgramData\SparTask\spartask-deploy\spartaskctl.exe hyperv ssh -- sudo /opt/spartask/spartaskctl -dir /opt/spartask status
C:\ProgramData\SparTask\spartask-deploy\spartaskctl.exe hyperv ssh    # sanal makinede komut satırı
```

Bu belgedeki diğer komutlar sanal makinenin içinde aynen çalışır.

## 3. Kurulumu kontrol edin

1. Adresi tarayıcıda açın; sertifika uyarısı olmamalı.
2. Yöneticiyle girin. Lisansla ilgili bir uyarı görünmemeli.
3. Stüdyodaki **Marketplace** bağlantısıyla SparTask hesabınıza girin ve bir modül (ör. CRM) kurun.
4. Modülün bir sürecini baştan sona çalıştırın.

## 4. Teslim almadan önce

- [ ] `/opt/spartask/.env` dosyasının bir kopyası şifre yöneticisinde. İçindeki `SECRET_KEY` bağlantı şifrelerinizi, `BACKUP_ENCRYPTION_KEY` yedekleri şifreler; `SPARTASK_INSTALLATION_KEY` lisansın bağlı olduğu anahtardır. Bu dosya olmadan yedekler açılamaz.
- [ ] Günlük yedek görevi kurulu (her gece 03:30, `/opt/spartask/backups/`).
- [ ] Yedeklerin sunucu dışında nereye kopyalanacağı belli. Aynı diskteki yedek sunucunun kaybına karşı korumaz.
- [ ] Yedekten geri yüklemenin ne zaman deneneceği belli (ör. üç ayda bir, başka bir sunucuya).
- [ ] E-posta gönderimi için SMTP ayarları yapıldı (aşağıda).
- [ ] Şirket sertifikası kullanılıyorsa bitiş tarihi biliniyor. Yenilendiğinde dosyaları `/opt/spartask/certs/cert.pem` ve `key.pem` olarak değiştirip `sudo ./spartaskctl restart` çalıştırılır.

## Günlük işletim

Komutlar sunucuda `/opt/spartask` klasöründe çalıştırılır (`cd /opt/spartask`).

| Ne yapmak istiyorsunuz? | Komut |
|---|---|
| Her şey çalışıyor mu? | `sudo ./spartaskctl status` |
| Logları izlemek (Ctrl+C ile çıkılır) | `sudo ./spartaskctl logs api` (ayrıca `worker`, `scheduler`, `migrate`, `caddy`, `db`) |
| Yeni sürüme geçmek | `sudo ./spartaskctl update 1.4.0` |
| Ayar değiştirmek | `.env`'i düzenleyin, sonra `sudo ./spartaskctl restart` |
| Eksik ayarları görmek | `sudo ./spartaskctl config -check` |
| Elle yedek almak | `sudo ./spartaskctl backup` |
| Yedekten geri yüklemek | `sudo ./spartaskctl restore backups/<zaman>` |
| Lisans durumu | `sudo ./spartaskctl license show` |
| Lisansı hemen yenilemek | `sudo ./spartaskctl license renew` |

**Ayarlar.** Her ayarın açıklaması `.env.example` dosyasındadır. `sudo ./spartaskctl config` ayarlarınızı paketle gelen şablonla karşılaştırır: yeni sürümle gelen ayarları ekler, üretilebilen şifreleri üretir, boş kalan zorunlu ve önerilen ayarları açıklamasıyla listeler. Şifre değerlerini hiçbir zaman ekrana yazmaz. `update` ve `restart` bu kontrolü kendisi yapar; zorunlu bir ayar eksikse hiçbir şeyi değiştirmeden durur.

**E-posta.** Davetlerin ve bildirimlerin gitmesi için `.env`'de `INVITATION_SMTP_HOST`, `INVITATION_SMTP_PORT`, `INVITATION_SMTP_USER`, `INVITATION_SMTP_PASSWORD` ve gönderen adresini (`INVITATION_SMTP_FROM`) doldurup `sudo ./spartaskctl restart` çalıştırın. Bazı sunucu sağlayıcıları 25, 465 ve 587 portlarını engeller; o durumda e-posta servisinizin 2525 portunu kullanın.

**Veritabanı.** PostgreSQL dışarıya açık değildir. Sunucuda bağlanmak için: `sudo docker compose exec db sh -c 'psql -U "$POSTGRES_USER" "$POSTGRES_DB"'`. Veriyi elle değiştirmeyin; firmalar ve süreçler tutarlılığı uygulama üzerinden korur.

## Güncelleme

```bash
cd /opt/spartask
sudo ./spartaskctl update 1.4.0
```

`update` sırasıyla:

1. Yeni sürümün imajlarını indirir; bu sırada eski sürüm çalışmaya devam eder. İnemezse hiçbir şey değişmez.
2. Yedek alır.
3. Yeni sürümü başlatır; veritabanı değişiklikleri (migration) bu sırada uygulanır.
4. Sağlık kontrolü yapar.

Yeni sürüm açılmazsa ayarlar eski sürüme döner ve komut ne yapılacağını yazar: önce `sudo ./spartaskctl restart` ile eski sürümü başlatın. Yeni sürümün migration'ları veritabanını değiştirdiyse eski sürüm açılmayabilir; o durumda komutun gösterdiği güncelleme öncesi yedeği `restore` ile geri yükleyin. Yedekten sonra yazılan veri kaybolacağı için bu kararı bilerek verin. Otomatik veritabanı geri yüklemesi yoktur.

**Kurulum paketini güncellemek** (yeni ayarlar ya da compose dosyaları geldiğinde):

```bash
cd /opt/spartask
curl -fsSL https://github.com/spartaskai/spartask-deploy/releases/latest/download/spartask-deploy-linux-amd64.tar.gz | sudo tar xz --strip-components=1
sudo ./spartaskctl config
sudo ./spartaskctl restart
```

`.env`, `data/`, `certs/` ve `backups/` olduğu gibi kalır.

## Yedek ve geri yükleme

Bir yedek `backups/<zaman>/` klasörüdür: veritabanı (`spartask.dump`), dosyalar (`storage.tar.gz`) ve ayarlar (`config.tar.gz`: `.env`, sertifikalar). Dosyalar `BACKUP_ENCRYPTION_KEY` ile şifrelenir (`.enc`). En yeni 14 yedek tutulur (`BACKUP_KEEP`).

Yedekleri düzenli olarak sunucu dışına kopyalayın (barındırma firmasının yedek alanı, S3 uyumlu depolama vb.; kişisel veri içerdiği için mümkünse Türkiye'de).

```bash
sudo ./spartaskctl restore backups/20261004T033000Z                # veriyi yedekteki hale getirir (onay ister)
sudo ./spartaskctl restore backups/20261004T033000Z -with-config   # yeni sunucuya taşırken: .env ve sertifikalar da
```

**Yeni sunucuya taşımak:** yeni sunucuya paketi açın (Güncelleme'deki `curl ... | sudo tar xz` komutu), yedek klasörünü `/opt/spartask/backups/` altına kopyalayın ve anahtarı şifre yöneticisinden vererek geri yükleyin:

```bash
sudo BACKUP_ENCRYPTION_KEY=<şifre yöneticisindeki değer> ./spartaskctl restore backups/<zaman> -with-config
```

Geri yükleme bitince SparTask kendiliğinden başlar.

Hyper-V'nin sanal makine yedeği SparTask yedeğinin yerine geçmez; tutarlı yedek `spartaskctl backup`'tır.

## Şirket ağındaki sistemlere bağlanmak

Güvenlik için firmaların süreçleri varsayılan olarak özel ağlara (10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7) bağlanamaz. Süreçleriniz şirket ağındaki bir veritabanına, e-posta sunucusuna ya da servise bağlanacaksa `.env`'e şunlardan birini yazıp `sudo ./spartaskctl restart` çalıştırın:

- Tüm şirket ağına izin (tek firmalı kurulumlar için):

  ```
  SPARTASK_EGRESS_ALLOW_PRIVATE=true
  ```

- Yalnızca belirli bir sisteme izin (`tenant` = firmanın şema adı; adres, port ve IP birlikte eşleşmelidir):

  ```
  SPARTASK_EGRESS_PRIVATE_GRANTS='[{"tenant":"tenant_firmaniz","host":"db.firmaniz.local","port":5432,"cidrs":["10.80.4.20/32"]}]'
  ```

SparTask'ın kendi servisleri (veritabanı, NATS), sunucunun kendisi ve bulut metadata adresleri her zaman kapalıdır. Engellenen bağlantı süreçte `OUTBOUND_TARGET_DENIED` hatası olarak görünür.

## Lisans

Ücretsiz on-prem lisansı tek firmalıktır ve 12 aylıktır. Kurulum lisansını aktivasyonda kendisi alır ve bitimine 30 gün kala kendisi yeniler. On-prem lisans sözleşmesi değiştiğinde firmanızın yöneticisi uygulamada yeni sürümü onaylayana kadar yenileme bekler. Yenileme başarısız olursa yöneticiler uygulamada uyarı görür.

Lisans `.env`'deki `SPARTASK_INSTALLATION_KEY`'e bağlıdır. `.env` kaybolursa yedekten geri yükleyin; geri yüklenemezse lisans servisinden yeni lisans gerekir.

## Sorun giderme

| Belirti | Ne yapmalı |
|---|---|
| Kurulum ön kontrolde duruyor | Her satır ne yapılacağını söyler. Giderip aynı komutu çalıştırın; hiçbir şey değişmemiştir |
| Kurulum yarıda kaldı | Nedeni giderip aynı komutu çalıştırın; kaldığı yerden sürer |
| "activation failed" | Kodun süresi dolmuş olabilir: sayfadaki **Yeni kod** ile alın ve komuttaki kodu değiştirin |
| İmajlar inmiyor | `ghcr.io` ve Docker Hub'a giden HTTPS'i açın; proxy varsa Docker'ın proxy ayarına da yazın |
| "a proxy on this network replaces certificates" | Şirket proxy'sinin kök sertifikasını sunucuya ekleyin (`/usr/local/share/ca-certificates/` + `sudo update-ca-certificates`) ve Docker'ı yeniden başlatın |
| Tarayıcıda sertifika uyarısı | Let's Encrypt: DNS kaydının sunucuyu gösterdiğini ve 80/443'ün internetten açık olduğunu kontrol edin. Şirket sertifikası: `certs/cert.pem` tam zincir olmalı ve adresi kapsamalı |
| SparTask açılmıyor | `sudo ./spartaskctl status`, sonra `sudo ./spartaskctl logs migrate` ve `sudo ./spartaskctl logs api` |
| E-posta gitmiyor | SMTP ayarlarını kontrol edin; 25/465/587 engelliyse 2525 portu |
| Süreç şirket ağındaki sisteme bağlanamıyor | [Şirket ağındaki sistemlere bağlanmak](#şirket-ağındaki-sistemlere-bağlanmak) |

## Paketteki dosyalar

| Dosya | Görevi |
|---|---|
| `install.sh`, `install.ps1` | Tek komutla kurulum (Linux, Windows) |
| `spartaskctl` | Kurulum ve işletim aracı (`install`, `update`, `backup`, `restore`, `status`, `logs`, `config`, `license`) |
| `compose.yml` | SparTask'ın servisleri: Caddy (HTTPS), PostgreSQL, NATS, `migrate`, API ve web arayüzü, worker, scheduler |
| `caddy/Caddyfile` | HTTPS ve yönlendirme ayarı |
| `.env.example` | Ayar şablonu ve her ayarın açıklaması |

```
Kullanıcılar ─► Caddy :443 ─► api (web arayüzü + API)
                 iç ağ: db (PostgreSQL) · nats · worker · scheduler · migrate
```

Dışarıya yalnızca 80 ve 443 açılır; veritabanı ve NATS yalnızca iç ağdadır. Her başlatmada `migrate` bekleyen veritabanı değişikliklerini uygular; başarısız olursa API eski haliyle kalır.
