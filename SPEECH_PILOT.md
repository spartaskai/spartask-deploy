# İngilizce sesli toplantı pilotu

Uygulama imajı: `1.6.1-rc.5`. Kurulum paketi: `v1.5.1-rc.4`.
Bu pilot GPU gerektirmez. Sunucuda yalnız STT çalışır (2 CPU/1 GiB üst sınır). Ajan cevaplarını her
katılımcının tarayıcısı kendi bilgisayarındaki sesle okur; sunucu TTS'i kapalıdır ve container'ı başlatılmaz.
Tek sesli oda ile başlayın.

RC.5, RC.4'te sunucu TTS'i kapalıyken süreç worker'ının açılışta çökmesini düzeltir (süreçler ilerlemiyordu).
RC.4'te geçici olarak `.env`'ye eklenen `SPEECH_TTS_URL` satırı artık gerekmez; `config -speech-local` onu temizler.

RC.4 pilot geri bildirimini giderir:

- **Ajan sesi tarayıcıda.** Ajan cevabı geldiği anda tarayıcı onu cümle cümle okur. Yalnız cihaz üstü
  sesler kullanılır (Chrome'un "Google …", Edge'in "… Online (Natural)" gibi internet sesleri hiç
  kullanılmaz); cevap metni bilgisayardan çıkmaz. "AI sesleri" düğmesiyle kapatılabilir. Bilgisayarda
  İngilizce yerel ses yoksa uyarı çıkar ve cevaplar yalnız yazılı görünür.
- **Hoparlör yankısı yazıya dökülmez.** Hoparlörden mikrofona sızan ajan cevabı, okunduğu süre boyunca
  kelime örtüşmesiyle tanınıp atılır (`voice_transcription outcome=echo`). Ajan konuşurken yakalanan ses
  yalnız açıkça söylenen ada hitap eder; ajan kendi cevabına cevap vermez.
- **Söz kesme.** Siz konuşunca, sunucu sesinizi yazıya döktüğü anda tarayıcı ajanı susturur. Bu,
  STT süresi kadar gecikir. Kulaklık yankıyı tamamen ortadan kaldırır.
- **Uydurma cümleler elenir.** Ses, Whisper'dan önce konuşma algılayıcıdan (Silero VAD) geçer; düşük
  güvenli parçalar (`SPEECH_STT_MIN_AVG_LOGPROB`, varsayılan -1.0) atılır. İngilizce olmayan konuşma ve
  oda gürültüsü sahte İngilizce cümleye dönüşmez. Kısa aksanlı cevaplar ("Thank you.") kaybolursa eşiği
  düşürün (ör. -1.2); sahte cümle sürerse yükseltin (ör. -0.8).

RC.3'ten devralınanlar: ajana adıyla seslenme (seçim gerekmez; adsız devam cümleleri son seslenilen ajana
30 saniye gider, `MEETING_VOICE_ADDRESS_WINDOW`), STT'de ajan adı ipucu ve gürültü döngüsü filtresi.

Teşhis (konuşma metni kaydedilmez):
- `docker logs --since 5m spartask-voice-1`: `voice_transcription outcome=posted|empty|echo|dropped_capacity|failed`,
  `addressed`, `audio_ms`, `processing_ms`.
- `docker logs --since 5m spartask-speech-stt-1`: `speech_transcription ... min_avg_logprob=... max_no_speech=...`
  ve `speech_inference elapsed_ms`.

## Kullanıcı bilgisayarı

Ajan sesini duymak için tarayıcının İngilizce bir **yerel** sesi olmalı. Toplantı sayfasında F12 > Console:

```js
speechSynthesis.getVoices().filter(v => v.localService).map(v => v.name + ' | ' + v.lang)
```

Listede `en-` ile başlayan bir ses yoksa Windows'ta Ayarlar > Saat ve dil > Konuşma > Ses ekle'den
İngilizce (ör. English (United States)) sesini kurup tarayıcıyı yeniden başlatın.

## Mevcut Linux sunucusunda güncelleme

Komutları `.env` ve `spartaskctl` bulunan **mevcut kurulum klasöründe** çalıştırın. Yeni kurulum
başlatmayın. Önce toplantıyı kapatın.

Sıra önemlidir: uygulama önce mevcut ses ayarlarıyla yeni sürüme geçer (güncelleme başarısız olursa önceki
sürüm aynı ayarlarla geri döner); yeni ses profili ve tarayıcı modu ancak bundan sonra uygulanır.

```bash
set -eu
test -f .env
test -f spartaskctl
pilot_tmp=$(mktemp -d)
pilot_release=https://github.com/spartaskai/spartask-deploy/releases/download/v1.5.1-rc.4
curl -fL "$pilot_release/spartask-deploy-linux-amd64.tar.gz" -o "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz"
curl -fL "$pilot_release/SHA256SUMS" -o "$pilot_tmp/SHA256SUMS"
(cd "$pilot_tmp" && grep ' spartask-deploy-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -)
tar -xzf "$pilot_tmp/spartask-deploy-linux-amd64.tar.gz" -C "$pilot_tmp"
sudo cp -p spartaskctl "spartaskctl.before-speech-$(date +%Y%m%d%H%M%S)"
sudo install -m 755 "$pilot_tmp/spartask-deploy/spartaskctl" ./spartaskctl
sudo install -m 644 "$pilot_tmp/spartask-deploy/.env.example" ./.env.example
sudo ./spartaskctl update 1.6.1-rc.5
sudo install -m 644 "$pilot_tmp/spartask-deploy/speech-profile.json" ./speech-profile.json
sudo ./spartaskctl config -speech-local
sudo ./spartaskctl config -check
sudo ./spartaskctl restart
sudo ./spartaskctl status
```

`update` varsayılan olarak uygulama yedeğini alır. `.env`, LiveKit domain/TLS/anahtarları ve veritabanı
korunur. `config -speech-local` STT'yi açık, sunucu TTS'ini kapalı ve `MEETING_VOICE_AGENT_SPEECH=browser`
yapar; mevcut yerel anahtarları korur. `restart` sonrası `speech-tts` container'ı çalışmaz.

Sunucuda ajan sesine geri dönmek için `.env` içinde `SPEECH_TTS_ENABLED=true` ve
`MEETING_VOICE_AGENT_SPEECH=server` yapıp `spartaskctl restart` çalıştırın (`config -speech-local`
tekrar çalıştırılırsa tarayıcı moduna döner).

## Daily kabulü

1. Sayfayı yenileyip yeni Daily toplantısı açın; mümkünse önce kulaklıkla, sonra hoparlörle deneyin.
2. Kamerasız veya görüntülü katılın. Mikrofonu açın. "AI sesleri: açık" ve yerel ses adının
   göründüğünü kontrol edin.
3. "Ada, what did you finish yesterday?" deyin. Metin dökümünü ve ajanın sesli cevabının ne kadar
   sürede başladığını kontrol edin.
4. Hoparlörle: ajan konuşurken sessiz kalın; ajanın cümleleri sizin adınıza yazıya dökülmemeli ve ajan
   kendine cevap vermemeli.
5. Adını söylemeden bir devam sorusu sorun; aynı ajan cevap vermeli. Ajan konuşurken araya girin;
   ajan susmalı.
6. Sessiz odada bir dakika bekleyin; uydurma cümle yazılmamalı.
7. Görüşmeden ayrılın; toplantıyı bitirin ve oda/işlerin kapandığını kontrol edin.

Kapatmak için `.env` içinde `SPEECH_ENABLED=false` ve `MEETING_VOICE_ENABLED=false` yapıp
`spartaskctl restart` çalıştırın. Video ve yazılı toplantı kullanılmaya devam eder.
