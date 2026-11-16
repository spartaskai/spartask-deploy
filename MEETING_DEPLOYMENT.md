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

Aktif toplantıları bitirin. Yeni release'in `upgrade-browser-speech.sh` scriptini kullanın.
Script eski speech işi veya executor tanımı varsa önce durur; geçmiş verileri silmez.
Binary/env/ilgili compose ve profil dosyalarını yedekler, bir geri dönüş scripti üretir.
Yeni config SPEECH_*, MEETING_VOICE_* ve SPARTASK_VOICE_IMAGE atamalarını kaldırır; tanımları
artık yeni env şablonunda bulunmaz. Yalnız tarayıcı ve LiveKit ayarları tutulur.

Uygulama update/restart işlemi eski voice, speech-stt ve speech-tts container'larını
--remove-orphans ile kaldırır. Son kontrol kalan hedef container'ları hata olarak bildirir.
Başarılı geçişte eski speech-profile.json ve SPEECH_PILOT.md kurulumdan kaldırılır.
Uygulanmış migration'lar, speech iş geçmişi ve meeting transkriptleri korunur.
