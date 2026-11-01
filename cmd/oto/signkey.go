package main

// otoMinisignPublicKey là minisign public key dùng để xác thực bản cập nhật
// --upgrade. Được tạo bằng `go run ./cmd/oto-sign gen` (hoặc minisign -G) và
// phải khớp private key đặt trong GitHub secret oto_MINISIGN_KEY của workflow.
//
// Để trống = bản build thủ công không nhúng key: --upgrade sẽ từ chối cập nhật
// trừ khi đặt oto_ALLOW_UNVERIFIED=1.
var otoMinisignPublicKey = `untrusted comment: minisign public key: 94B14B11A60C8E01
RWQBjgymEUuxlK0CoT35veUlfx1H4EJDkuLuZYZzhXhNa7q4YlGPS2MY`
