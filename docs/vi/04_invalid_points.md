# ❌ Điểm Không Hợp Lý (Invalid / Overreaching Points)
> 🇬🇧 [English version](../04_invalid_points.md)
> Những luận điểm **sai**, **phóng đại**, hoặc **tạo ra kỳ vọng không thực tế**
> trong cả hai bản báo cáo.

### 📌 Trạng thái hiện tại (v0.1.5)

> Một số điểm đã được giải quyết:
> - **#5 Wire**: axe dùng **manual wiring** trong `main.go` — xác nhận phê phán là đúng
> - **#7 pgx**: Đã tích hợp pgx v5 (`pkg/db/postgres/adapter.go`)

---

## 1. "Khả năng Mở Rộng Vô Hạn" — Phóng Đại Nguy Hiểm

**Trích từ báo cáo 1:**
> "một nền tảng tường minh và có **khả năng mở rộng vô hạn**"

**Tại sao sai:**
Không có hệ thống nào có khả năng mở rộng vô hạn. Tuyên bố này là marketing language, không phải engineering statement.

**Vấn đề thực tế:**
- Go xử lý concurrency tốt hơn Ruby, nhưng **database vẫn là bottleneck chính**
- Connection pool của PostgreSQL có giới hạn (~500–1000 connections thực tế)
- Network bandwidth, disk I/O, memory đều có giới hạn vật lý
- Golang binary chạy trên 1 core nếu không set GOMAXPROCS đúng

**Phiên bản trung thực:**
> "Go cho phép scale horizontal hiệu quả hơn Ruby do lightweight goroutines và static binary deployment, nhưng bottleneck chuyển sang tầng database và infrastructure sớm hơn."

---

## 2. Loại Bỏ Hoàn Toàn GORM — Thiếu Nuance

**Trích từ báo cáo 1:**
> "GORM bắt buộc phải bị loại khỏi bản thiết kế kiến trúc"

**Tại sao không hợp lý:**
GORM có những use case hợp lệ:
- Internal admin tools không cần hiệu năng cao
- Rapid prototyping feature mới trước khi optimize
- Team nhỏ với deadline MVP gấp

**Vấn đề của việc cấm tuyệt đối:**
- Tạo ra "political debt" — dev sẽ dùng GORM ngầm rồi giấu
- Mất đi tactical flexibility

**Phiên bản hợp lý hơn:**
```markdown
## GORM Usage Policy:
❌ FORBIDDEN in: core business logic, high-traffic endpoints
⚠️  ALLOWED in: admin scripts, one-off data migrations, internal tooling
✅  PREFERRED: Ent (mutations) + sqlc (queries)
```

---

## 3. "Hiệu Năng Tối Đa" Của sqlc — Không Đủ Context

**Trích từ báo cáo 1:**
> "sqlc: Hiệu năng tối đa, chỉ bị giới hạn bởi tốc độ mạng và trình điều khiển"

**Tại sao misleading:**
- sqlc chỉ giúp tầng **code** không có overhead
- Hiệu năng thực tế phụ thuộc vào **query quality** — index missing, N+1 vẫn xảy ra với sqlc
- `pgx` driver (thay vì `database/sql`) còn nhanh hơn nhiều nhưng không được đề cập

**Thực tế:**
```
Bottleneck thứ tự quan trọng:
1. Missing indexes          ← quan trọng nhất
2. N+1 query problems       ← rất phổ biến
3. Connection pool config   ← sering bị bỏ quên
4. Network latency (DB location)
5. ORM/library overhead     ← GORM ~5-10%, sqlc ~0%
```
→ Library choice là **bước cuối** trong chuỗi tối ưu hiệu năng.

---

## 4. "Circular Dependency Là Lỗi Kiến Trúc Của MVC" — Partially Wrong

**Trích từ báo cáo 1:**
> "việc tái hiện cấu trúc thư mục của Rails là hành động tự sát về mặt kiến trúc"

**Tại sao quá đà:**
- Circular dependency là **vấn đề thiết kế**, không phải vấn đề của folder structure
- MVC folder structure không tự nhiên tạo circular import
- Circular import xảy ra khi **package responsibilities không rõ ràng**, bất kể folder nào

**Ví dụ minh chứng:**
```
Folder structure: controllers/ + models/ + views/
→ KHÔNG có circular import nếu:
  controllers import models ✅
  models KHÔNG import controllers ✅
  views KHÔNG import controllers ✅
```

**Phiên bản chính xác hơn:**
> "MVC folder structure trong Go có nguy cơ cao dẫn đến circular import nếu team không hiểu rõ dependency direction. Clean Architecture giải quyết vấn đề này bằng cách enforce direction tường minh hơn."

---

## 5. Wire — Được Trình Bày Như Silver Bullet

**Trích từ báo cáo 1:**
> "Nếu dự án phình to... nền tảng có thể tích hợp Google Wire"

**Tại sao murky:**
- Wire có learning curve cao và debugging phức tạp
- Wire-generated code dài và khó đọc
- Wire không giải quyết được tất cả DI scenarios (circular deps trong DI graph)
- Nhiều large Go projects (k8s ecosystem) **không dùng Wire**

**Thực tế:**
Với project < 50 endpoints: `main.go` manual wiring hoàn toàn đủ.
Wire chỉ cần thiết khi > 100+ components cần wire.

---

## 6. Báo Cáo 2 — "10 Lỗ Hổng" Nhưng Ưu Tiên Sai

**Trích từ báo cáo 2:**
> 10 lỗ hổng được liệt kê theo thứ tự: Transaction model, Outbox, Domain boundary...

**Vấn đề thứ tự ưu tiên:**
Báo cáo 2 đặt "Transaction model" là critical nhất — đúng. Nhưng đặt "Developer adoption" (#8) và "Observability" (#7) — **đây là thứ tự sai với thực tế**.

**Thứ tự đúng theo impact:**
```
1. Transaction model          → data corruption ngay lập tức
2. Error taxonomy             → API unusable ngay lập tức
3. Developer adoption         → team không dùng được kiến trúc
4. Observability              → production incident không debug được
5. Outbox pattern             → data inconsistency (chỉ khi dùng queue)
6. CQRS light                 → performance degradation (gradual)
7. Domain boundary            → architectural decay (long-term)
8. Failure strategy           → production incident (eventually)
9. Performance/caching        → scale issue (later stage)
10. Deployment model          → devops concern (separate team)
```

---

## 7. Không Nhắc Đến pgx — Thiếu Sót Quan Trọng

**Vấn đề:**
Cả 2 báo cáo đề cập `database/sql` standard library nhưng không nhắc đến `pgx` (jackc/pgx).

**Thực tế:**
- `pgx` là PostgreSQL driver được recommend cho production Go apps
- Native PostgreSQL types support (JSONB, arrays, custom types)
- Performance cao hơn `database/sql` built-in driver
- sqlc, Ent đều support pgx natively

**Đã giải quyết trong v0.1.5** — axe dùng pgx v5 qua `pkg/db/postgres/adapter.go`.

---

## Tóm tắt Điểm Không Hợp Lý

```
❌ "Mở rộng vô hạn"      → marketing, không phải engineering
❌ Cấm tuyệt đối GORM     → thiếu nuance, tạo workarounds ngầm
❌ sqlc "hiệu năng tối đa"→ misleading, query quality mới quan trọng
❌ MVC = circular import  → quá đà, vấn đề là package design
✅ Wire = silver bullet   → axe dùng manual wiring, xác nhận phê phán đúng
❌ Priority order báo cáo 2 → sắp xếp chưa đúng real-world impact
✅ pgx                    → đã tích hợp pgx v5 (pkg/db/postgres)
```

> Tài liệu này giữ nguyên nội dung gốc để làm **bối cảnh lịch sử** cho các quyết định kiến trúc.
