# Görüntülü toplantı ve tarayıcı konuşması

Video ve insan sesi LiveKit üzerinden taşınır. Konuşma tanıma (STT) ve agent seslendirme (TTS)
katılımcıların tarayıcısında çalışır; sunucu model servisi veya native voice daemon yoktur.

## Kurulum

1. `spartaskctl config -meeting` ile yerel veya harici LiveKit bağlantısını hazırlayın.
2. `spartaskctl config -speech-browser` ile tarayıcı konuşmasını açın.
3. `spartaskctl config -check` ardından `spartaskctl restart` çalıştırın.

Yerel LiveKit için LIVEKIT_URL, LIVEKIT_PUBLIC_IP ve digest'e sabitlenmiş LIVEKIT_IMAGE gerekir.
DNS/TLS ve WebRTC TCP/UDP erişimini hazırlayın. Kısıtlı ağlarda TURN sağlayan harici LiveKit
kullanılabilir. LiveKit anahtarları gizlidir; env ve yedeklerin erişim izinlerini koruyun.

Browser ayarları: MEETING_BROWSER_SPEECH_ENABLED ve MEETING_BROWSER_SPEECH_LANGUAGE (tr/en/de).
Mikrofon açıldığında STT başlar. Agent konuşurken tanıma durur; söz almak için agentı durdurun.
Her konuşmacının son agent hedefi toplantı boyunca korunur. Farklı agent adı hedefi değiştirir;
bir insana hitap hedefi temizler. Mikrofon kapatılınca tanıma durur. İkinci sekme tek aktif
konuşma sahibi kilidini bekler. Tarayıcı tanıma desteklemiyorsa metinle devam edilir.

Tarayıcı sistemin varsayılan mikrofonunu ve sağlayıcısının çevrimiçi tanıma hizmetini kullanabilir.
Tamamen çevrimdışı çalışma garantisi yoktur. Tarayıcı TTS sesi merkezi bir SFU agent audio track'i
olarak yayımlanmaz; bir metnin kaydı herkesin onu duyduğunu kanıtlamaz.

## Eski sunucu speech kurulumunun kaldırılması

Yalnız sunucu speech pilotu (v1.5.1-rc.1 – rc.5 paketleri) kurulmuş sunucular içindir.
Aktif toplantıları bitirin; `.env` ve `spartaskctl` dosyalarını yedekleyin. Ardından yeni
paketin `spartaskctl` binary'si ve `.env.example` dosyasıyla sırasıyla
`config -speech-browser`, `config -check` ve `update <sürüm>` çalıştırın.

Config SPEECH_*, MEETING_VOICE_* ve SPARTASK_VOICE_IMAGE atamalarını kaldırır; tanımları
artık env şablonunda bulunmaz. Yalnız tarayıcı ve LiveKit ayarları tutulur.
Update/restart eski voice, speech-stt ve speech-tts container'larını --remove-orphans ile
kaldırır; `docker compose ps -a` ile kalmadığını doğrulayın. Eski speech-profile.json ve
SPEECH_PILOT.md dosyaları artık kullanılmaz, elle silinebilir.
Uygulanmış migration'lar, speech iş geçmişi ve meeting transkriptleri korunur.

Sunucu speech imajları (spartask-voice, spartask-speech-stt, spartask-speech-tts) artık
yayımlanmaz. Bu imajlara bağlı eski pilot paketleri yeniden kurulamaz.
