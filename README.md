# auth-pddikti

Microservice bridge dan HTTP client Go untuk verifikasi identitas dan status aktif mahasiswa Indonesia berbasis PDDIKTI (Kemdiktisaintek RI).

Layanan ini berfungsi sebagai lapisan verifikasi data (student verification / e-KYC layer) yang dirancang untuk melengkapi alur autentikasi aplikasi utama. Sistem memvalidasi keaktifan akademik, menarik identitas resmi (nama lengkap, NIM, perguruan tinggi, program studi, jenjang, status semester), dan menyediakan pencarian sivitas akademika secara on-demand tanpa penambangan data massal.

## Karakteristik Teknis

- On-demand single lookup: Menghindari beban scraping skala besar dengan mengeksekusi request langsung saat user mendaftar.
- Role & eligibility verification: Memvalidasi keabsahan status mahasiswa aktif sebelum hak akses atau role diberikan oleh sistem utama.
- Header compliance: Menyertakan header origin, referer, dan user-agent peramban untuk mencegah blokir HTTP 403 dari WAF upstream.
- Deterministic match: Pencarian berbasis NIM memetakan satu mahasiswa spesifik secara presisi.
- Cross-origin resource sharing: Mendukung integrasi langsung dari antarmuka web maupun mobile.
- Cakupan nasional: Berlaku untuk seluruh perguruan tinggi negeri, swasta, dan politeknik di Indonesia.

## Catatan Arsitektur

Layanan ini melakukan verifikasi data terhadap pangkalan data nasional secara read-only. Untuk menegakkan kepemilikan akun (identity ownership) secara mutlak, layanan ini direkomendasikan untuk dipasangkan bersama mekanisme autentikasi utama seperti Google Workspace OAuth institusi (`@kampus.ac.id`) atau verifikasi email kampus.

## Persyaratan

- Go 1.22 atau lebih baru

## Menjalankan Layanan

```bash
git clone https://github.com/akbarhann/auth-pddikti.git
cd auth-pddikti
go mod tidy
go run main.go
```

Secara default layanan berjalan pada port 8090. Port dapat disesuaikan melalui environment variable:

```bash
PORT=8090 go run main.go
```

## Spesifikasi API

### 1. Verifikasi dan Pengambilan Data Mahasiswa (Berdasarkan NIM)

Mengambil data mahasiswa secara langsung dan mengembalikan status keaktifan semester berjalan.

- Method: GET atau POST
- Endpoint: `/api/verify/:nim` atau `/api/verify`

Contoh request GET:

```bash
curl -s http://localhost:8090/api/verify/23010000001
```

Contoh request POST:

```bash
curl -s -X POST http://localhost:8090/api/verify \
  -H "Content-Type: application/json" \
  -d '{"nim": "23010000001"}'
```

Contoh response (200 OK):

```json
{
  "is_active": true,
  "status": "success",
  "student": {
    "jenis_kelamin": "L",
    "jenjang": "Sarjana",
    "kampus": "Universitas Contoh Indonesia",
    "kode_pt": "001001",
    "nama": "CONTOH MAHASISWA",
    "nim": "23010000001",
    "prodi": "Teknik Informatika",
    "status_akademik": "Aktif-2026/2027 Ganjil",
    "tanggal_masuk": "2023-08-15"
  },
  "verified": true
}
```

### 2. Pencarian Kandidat Mahasiswa (Berdasarkan Nama atau NIM)

Mengembalikan daftar mahasiswa yang cocok dengan kata kunci pencarian.

- Method: GET
- Endpoint: `/api/search?q={keyword}`

Contoh request:

```bash
curl -s "http://localhost:8090/api/search?q=budi%20santoso"
```

Contoh response (200 OK):

```json
{
  "data": [
    {
      "id": "sample_encrypted_id_hash_value==",
      "nama": "BUDI SANTOSO",
      "nama_prodi": "TEKNIK INFORMATIKA",
      "nama_pt": "UNIVERSITAS CONTOH INDONESIA",
      "nim": "23010000001",
      "sinkatan_pt": "UCI"
    }
  ],
  "query": "budi santoso",
  "status": "success",
  "total": 1
}
```

### 3. Detail Mahasiswa Berdasarkan ID Terenkripsi

- Method: GET
- Endpoint: `/api/detail/:id`

Contoh request:

```bash
curl -s "http://localhost:8090/api/detail/sample_encrypted_id_hash_value=="
```

## Batasan Operasional Upstream

- Rate limit upstream PDDIKTI berada pada batas 60 request per menit per alamat IP publik.
- Layanan ini diperuntukkan bagi verifikasi alur registrasi/autentikasi pengguna secara bertahap, bukan untuk penarikan data massal (bulk harvesting).
