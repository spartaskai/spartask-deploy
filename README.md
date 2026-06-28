# Spartask Kolay Kurulum Kılavuzu

Bu depo, Spartask uygulamasını Windows veya Linux (Ubuntu) sunucularda kaynak kodlara ihtiyaç duymadan hızlıca ayağa kaldırmak için gerekli tüm dağıtım dosyalarını içerir.

## Depo İçeriği
- `spartask-installer`: Linux (Ubuntu) için interaktif/sessiz kurulum programı.
- `spartask-installer.exe`: Windows için web tabanlı görsel kurulum programı.
- `docker-compose.prod.yml`: Üretim ortamı için Docker servis tanımları.
- `migrations/`: Veritabanı tablolarını otomatik oluşturmak için gerekli SQL şemaları.

---

## 🐧 Linux (Ubuntu) Kurulum Adımları

### 1. Docker ve Docker Compose Kurulumu
Eğer sunucunuzda Docker kurulu değilse, terminalden şu komutlarla hızlıca kurabilirsiniz:
```bash
sudo apt update
sudo apt install -y docker.io docker-compose
sudo systemctl start docker
sudo systemctl enable docker
```

### 2. Kurulum Dosyalarını Çekme
Bu depoyu sunucunuza klonlayın:
```bash
git clone https://github.com/mkozan/spartask-deploy.git
cd spartask-deploy
```

### 3. Docker Hub Girişi
Private imajı (`mkozanimages/spartask:latest`) çekebilmek için Docker Hub hesabınızla giriş yapın:
```bash
docker login -u mkozanimages
```
*Şifre sorulduğunda Docker Hub'dan oluşturduğunuz **Kişisel Erişim Anahtarınızı (PAT)** yapıştırıp Enter'a basın.*

### 4. Kurulum Sihirbazını Çalıştırma
Kurulum dosyasına çalıştırma izni verin ve interaktif kurulumu başlatın:
```bash
chmod +x spartask-installer
./spartask-installer -cli
```
*Yükleyici size gerekli soruları (Uygulama Portu, Gemini API Key vb.) soracak, `.env` dosyasını oluşturacak ve tüm servisleri arka planda ayağa kaldıracaktır.*

---

## 🪟 Windows Kurulum Adımları

1. Bu depoyu bilgisayarınıza indirin (ZIP olarak indirip bir klasöre çıkartabilirsiniz).
2. Bilgisayarınızda **Docker Desktop** uygulamasının açık olduğundan emin olun.
3. Klasör içindeki `spartask-installer.exe` dosyasına çift tıklayın.
4. Tarayıcınızda otomatik olarak açılacak olan kurulum sihirbazındaki adımları takip edin.

---

## 🔄 Sunucu Yönetim Komutları

Uygulama sunucuda çalışırken kullanabileceğiniz temel yönetim ve bakım komutları aşağıdadır:

### 1. Uygulamayı Durdurma ve Başlatma
* **Uygulamayı Durdurmak İçin:**
  ```bash
  docker compose -f docker-compose.prod.yml down
  ```
* **Uygulamayı Arka Planda Başlatmak İçin:**
  ```bash
  docker compose -f docker-compose.prod.yml up -d
  ```
* **Uygulama Loglarını Anlık İzlemek İçin:**
  ```bash
  docker compose -f docker-compose.prod.yml logs -f
  ```

### 2. Veritabanını Sıfırlama (Tabloları Yeniden Oluşturma)
Herhangi bir sebepten ötürü veritabanını sıfırlamak ve `migrations/` altındaki SQL dosyalarını baştan çalıştırarak tabloları yeniden oluşturmak isterseniz sırasıyla şu komutları çalıştırın:

> [!CAUTION]
> Bu işlem veritabanındaki tüm verileri kalıcı olarak silecektir.

```bash
# 1. Servisleri durdurun
docker compose -f docker-compose.prod.yml down

# 2. Eski verileri tamamen temizleyin
sudo rm -rf ./postgres-data

# 3. Servisleri yeniden başlatın (SQL dosyaları otomatik olarak baştan çalıştırılacaktır)
docker compose -f docker-compose.prod.yml up -d
```
