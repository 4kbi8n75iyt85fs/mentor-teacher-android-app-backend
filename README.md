# Mentor Teacher Backend API

Go backend API for the Mentor Teacher mobile app.

## Deployment
- **Platform**: Koyeb (auto-deploys from GitHub)
- **URL**: https://spare-ardisj-665ghh6r-673c35eb.koyeb.app
- **Database**: PostgreSQL (Neon)

## Environment Variables (Koyeb)
```
DATABASE_URL=postgresql://...
IMGBB_API_KEY=your_imgbb_api_key  # For image hosting
```

## API Endpoints

### Core
- `GET /health` - Health check
- `GET /api/transactions` - Get transactions
- `POST /api/transactions` - Create transaction

### Teachers & Students
- `GET /api/teachers/:teacherId/schedules` - Get teacher's schedules
- `POST /api/chapters` - Create chapter
- `GET /api/chapters/:subscriptionId` - Get chapters

### Attendance
- `POST /api/attendance` - Record attendance
- `GET /api/attendance/:teacherId` - Get attendance history

### Manual Grading System (ImgBB + Admin Review)
- `POST /api/upload/image` - Upload image to ImgBB
- `POST /api/answer-papers/submit` - Submit answer paper for grading
- `GET /api/answer-papers` - List answer papers
- `GET /api/answer-papers/:id` - Get single answer paper
- `GET /api/admin/grading` - Get papers pending grading (admin)
- `POST /api/admin/grading/:id` - Save grade (admin)
- `GET /api/teacher/grades/:teacherId` - Get grading history (teacher)

### Analytics
- `GET /api/analytics/attendance` - Attendance analytics
- `GET /api/analytics/classes` - Class analytics

## Database Schema

### answer_papers table
```sql
mentor.answer_papers (
    id SERIAL PRIMARY KEY,
    subscription_id INTEGER,
    teacher_id VARCHAR(50) NOT NULL,
    student_name VARCHAR(255) NOT NULL,
    class_name VARCHAR(50) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    chapter_number INTEGER,
    chapter_name VARCHAR(255),
    image_urls TEXT,           -- JSON array of ImgBB URLs
    question_text TEXT,        -- Admin fills this
    total_marks INTEGER,       -- Admin fills this
    actual_marks INTEGER,      -- Admin fills this
    admin_suggestions TEXT,    -- Admin fills this
    status VARCHAR(50) DEFAULT 'pending',  -- pending/graded
    graded_at TIMESTAMP,
    graded_by VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW()
)
```

## Grading Flow
1. Teacher submits answer paper photos → uploaded to ImgBB → saved to DB (status: pending)
2. Admin opens grading dashboard → sees pending papers with image links
3. Admin manually grades: enters question, marks, suggestions
4. Teacher sees grades in app history

## Development
```bash
go mod download
go run main.go
```

## Last Updated
2026-02-06: Switched from AI grading to manual grading system with ImgBB image hosting
