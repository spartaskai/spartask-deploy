# İngilizce sesli toplantı pilotu

Uygulama imajı: `1.6.1-rc.3`. Kurulum paketi: `v1.5.1-rc.2`.
Bu pilot GPU gerektirmez. STT 2 CPU/1 GiB, TTS 2 CPU/2 GiB üst sınırı kullanır. Ortak inference
tek eşzamanlıdır; STT ve TTS aynı anda model çalıştırmaz, toplam üst sınır 4 çekirdekli sunucuya sığar.
Tek sesli oda ile başlayın.

RC.3 pilot geri bildirimini giderir:

- **Ajana adıyla seslenme.** Arayüzde seçim gerekmez. "Ada, what did you finish yesterday?" gibi
  ajanın tam adını ya da (başka katılımcıyla paylaşılmıyorsa) ilk adını söyleyin. Adsız devam
  cümleleri, son seslenilen ajana 30 saniye boyunca gider (`MEETING_VOICE_ADDRESS_WINDOW`). Pencereyi
  ajanın sesli cevabı yeniler; bir insanın adını söylemek pencereyi kapatır. Arayüz seçimi de çalışır.
- **Cevap cümle cümle seslendirilir.** İlk ses, tüm cevap yerine ilk cümle üretilince başlar. Geliştirme
  makinesinde (VPS'ten hızlı) üç cümlelik cevabın ilk sesi yaklaşık 3 saniyede, söz kesmeden hemen
  sonraki cevapta yaklaşık 5 saniyede geldi; VPS'te daha uzun olabilir. `processing_ms` artarsa TTS
  için `SPEECH_TTS_CPUS=1` deneyin. Ajanlara sesli toplantıda en fazla
  üç kısa cümleyle cevap vermeleri söylenir.
- **Söz kesme modeli yeniden yüklemez.** Ajanın sözü kesildiğinde TTS modeli kapanıp yeniden
  ısınmaz; sonraki cevap gecikmez.
- **Gürültü yazıya dökülmez.** Uğultu, cızırtı ve sessizlikten üretilen tekrarlı sahte metinler
  STT'de elenir.
- **Ses kapanmaz.** Ajan konuşurken kapasite bekleyen bir cümle düşerse yalnız o cümle atlanır; toplantının
  sesi kapanmaz.

RC.2'den devralınanlar: STT tek geçişte çözümler, ses parçası üst sınırı 6 saniye, mesaj kontrol aralığı
250 ms'dir. Önceden açıkça tanımlanmış `MEETING_VOICE_MAX_SEGMENT_MS` / `MEETING_VOICE_POLL` değerleri
varsayılanların önüne geçer. Bu ölçümler canlı VPS için gecikme garantisi değildir.

Teşhis: `docker logs --since 5m spartask-voice-1` içindeki `voice_transcription` satırı `outcome`,
`audio_ms`, `queue_ms`, `processing_ms` alanlarını gösterir. STT/TTS container'larındaki `speech_inference`
satırı modelin `elapsed_ms` süresini gösterir. Konuşma metni kaydedilmez.

## Mevcut Linux sunucusunda güncelleme

Komutları `.env` ve `spartaskctl` bulunan **mevcut kurulum klasöründe** çalıştırın.
Yeni kurulum başlatmayın. Önce toplantıyı kapatın. Registry'deki yeni speech paketleri Public
olmalı; private tutuluyorsa sunucu uygun registry oturumuna sahip olmalıdır.

```bash
set -eu
test -f .env
test -f spartaskctl
pilot_tmp=$(mktemp -d)
pilot_release=https://github.com/spartaskai/spartask-deploy/releases/download/v1.5.1-rc.2
curl -fL "$pilot_release/spartask-deploy-linux-amd64.tar.gz" -o "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz"
curl -fL "$pilot_release/SHA256SUMS" -o "$pilot_tmp/SHA256SUMS"
(cd "$pilot_tmp" && grep ' spartask-deploy-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -)
tar -xzf "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz" -C "$pilot_tmp"
sudo cp -p spartaskctl "spartaskctl.before-speech-$(date +%Y%m%d%H%M%S)"
sudo install -m 755 "$pilot_tmp/spartask-deploy/spartaskctl" ./spartaskctl
sudo install -m 644 "$pilot_tmp/spartask-deploy/speech-profile.json" ./speech-profile.json
sudo install -m 644 "$pilot_tmp/spartask-deploy/.env.example" ./.env.example
sudo ./spartaskctl config -speech-local
sudo ./spartaskctl update 1.6.1-rc.3
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
3. Ajan sesinin hazır olduğunu kontrol edin. AI çalışanı arayüzde seçmeyin.
4. Ajanın adıyla başlayan kısa bir İngilizce cümle söyleyin ("Ada, what did you finish yesterday?").
   Metin dökümünü, ilk sesin gelme süresini ve ajanın İngilizce sesli yanıtını kontrol edin. Adını
   söylemeden bir devam sorusu sorun; aynı ajan cevap vermelidir. Ajan konuşurken araya girin;
   ajan susmalı, ses kapanmamalıdır.
5. Yazılı sohbetin masaüstünde görüntünün yanında kullanılabildiğini kontrol edin.
6. Görüşmeden ayrılın; toplantıyı bitirin ve oda/işlerin kapandığını kontrol edin.

RC.1 yerel gerçek model kabulünde 16/16 kontrol geçti: sentetik TTS→STT, bearer doğrulama, sınırlar,
uzun metin ve TCP bağlantısı kesildikten sonra toparlanma. RC.3'te bağlantı kesilince model yeniden
yüklenmez; `acceptance.py` bunu `tts_network_cancellation_keeps_model` ile denetler. TTS tepe belleği yaklaşık 1,36 GiB;
1000 karakter/57 saniyelik ses yaklaşık 55 saniyede üretildi. Gerçek mikrofon, tarayıcı ve VPS
kapasitesi kullanıcı kabulünde ölçülür. Kısa ajan yanıtları konuşma akışını hızlandırır.

Kapatmak için `.env` içinde `SPEECH_ENABLED=false` ve `MEETING_VOICE_ENABLED=false` yapıp
`spartaskctl restart` çalıştırın. Video ve yazılı toplantı kullanılmaya devam eder.
