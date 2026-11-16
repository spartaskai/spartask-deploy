# İngilizce sesli toplantı pilotu

Uygulama imajı: `1.6.1-rc.1`. Kurulum paketi: `v1.5.1-rc.1`.
Bu pilot GPU gerektirmez. STT 2 CPU/1 GiB, TTS 1 CPU/2 GiB üst sınırı kullanır.
Tek sesli oda ve tek eşzamanlı inference ile başlayın.

## Mevcut Linux sunucusunda güncelleme

Komutları `.env` ve `spartaskctl` bulunan **mevcut kurulum klasöründe** çalıştırın.
Yeni kurulum başlatmayın. Önce toplantıyı kapatın. Registry'deki yeni speech paketleri Public
olmalı; private tutuluyorsa sunucu uygun registry oturumuna sahip olmalıdır.

```bash
set -eu
test -f .env
test -f spartaskctl
pilot_tmp=$(mktemp -d)
pilot_release=https://github.com/spartaskai/spartask-deploy/releases/download/v1.5.1-rc.1
curl -fL "$pilot_release/spartask-deploy-linux-amd64.tar.gz" -o "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz"
curl -fL "$pilot_release/SHA256SUMS" -o "$pilot_tmp/SHA256SUMS"
(cd "$pilot_tmp" && grep ' spartask-deploy-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -)
tar -xzf "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz" -C "$pilot_tmp"
sudo cp -p spartaskctl "spartaskctl.before-speech-$(date +%Y%m%d%H%M%S)"
sudo install -m 755 "$pilot_tmp/spartask-deploy/spartaskctl" ./spartaskctl
sudo install -m 644 "$pilot_tmp/spartask-deploy/speech-profile.json" ./speech-profile.json
sudo install -m 644 "$pilot_tmp/spartask-deploy/.env.example" ./.env.example
sudo ./spartaskctl update 1.6.1-rc.1
sudo ./spartaskctl config -speech-local
sudo ./spartaskctl config -check
sudo ./spartaskctl restart
sudo ./spartaskctl status
```

`update` varsayılan olarak uygulama yedeğini alır. `.env`, mevcut LiveKit domain/TLS/anahtarları
ve veritabanı bu dosya kopyalama adımında korunur. `config -speech-local` iki bağımsız speech
anahtarı üretir; tekrar çalıştırıldığında yerel anahtarları korur. Model/voice/imaj/adresleri
profil belirler. Cloudflare veya yeni speech portu açılması gerekmez; sağlayıcılar Docker iç ağındadır.

## Daily kabulü

1. Sayfayı yenileyip yeni Daily toplantısı açın.
2. Kamerasız veya görüntülü katılın. Başlangıçta mikrofon kapalıdır; açıkça açın.
3. Sesleneceğiniz AI çalışanlarını seçin; ajan sesinin hazır olduğunu kontrol edin.
4. İngilizce kısa bir cümle söyleyin. Metin dökümünü ve ajanın İngilizce sesli yanıtını kontrol edin.
5. Yazılı sohbetin masaüstünde görüntünün yanında kullanılabildiğini kontrol edin.
6. Görüşmeden ayrılın; toplantıyı bitirin ve oda/işlerin kapandığını kontrol edin.

Yerel gerçek model kabulünde 16/16 kontrol geçti: sentetik TTS→STT, bearer doğrulama, sınırlar,
uzun metin ve TCP bağlantısı kesildikten sonra toparlanma. TTS tepe belleği yaklaşık 1,36 GiB;
1000 karakter/57 saniyelik ses yaklaşık 55 saniyede üretildi. Gerçek mikrofon, tarayıcı ve VPS
kapasitesi kullanıcı kabulünde ölçülür. Kısa ajan yanıtları konuşma akışını hızlandırır.

Kapatmak için `.env` içinde `SPEECH_ENABLED=false` ve `MEETING_VOICE_ENABLED=false` yapıp
`spartaskctl restart` çalıştırın. Video ve yazılı toplantı kullanılmaya devam eder.
