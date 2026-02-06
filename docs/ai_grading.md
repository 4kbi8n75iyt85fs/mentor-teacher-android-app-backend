# AI Exam Grading Implementation

## Overview
Implementing AI-powered exam grading using Google Gemini 1.5 Flash (free tier: 1500 requests/day).

## Database Schema

### New Table: `exam_submissions`
```sql
CREATE TABLE mentor.exam_submissions (
    id SERIAL PRIMARY KEY,
    subscription_id INTEGER REFERENCES mentor.subscriptions(id),
    teacher_id VARCHAR(50) NOT NULL,
    student_name VARCHAR(255) NOT NULL,
    class INTEGER NOT NULL,
    subject VARCHAR(255) NOT NULL,
    chapter_number INTEGER,
    question_text TEXT,
    image_data TEXT,  -- Base64 encoded image
    ai_score INTEGER,  -- 0-100
    ai_feedback TEXT,  -- AI generated feedback
    ai_suggestions TEXT,  -- Improvement suggestions
    teacher_notes TEXT,  -- Teacher's additional notes
    status VARCHAR(50) DEFAULT 'pending',  -- pending, graded, reviewed
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_exam_submissions_teacher ON mentor.exam_submissions(teacher_id);
CREATE INDEX idx_exam_submissions_student ON mentor.exam_submissions(student_name);
CREATE INDEX idx_exam_submissions_status ON mentor.exam_submissions(status);
```

## API Endpoints

### POST /api/exam/submit
Submit answer paper for AI grading

Request:
```json
{
  "subscription_id": 1,
  "teacher_id": "1001",
  "student_name": "Student Name",
  "class": 2,
  "subject": "Mathematics",
  "chapter_number": 1,
  "question_text": "Optional: The question being answered",
  "image_base64": "base64 encoded image data"
}
```

Response:
```json
{
  "success": true,
  "submission_id": 123,
  "score": 85,
  "feedback": "Good understanding of concepts...",
  "suggestions": "Practice more on..."
}
```

### GET /api/exam/submissions
Get grading history

### GET /api/exam/submissions/:id
Get specific submission details

## Gemini API Integration

Using Gemini 1.5 Flash for:
- Handwriting recognition (OCR)
- Answer evaluation against question
- Score assignment (0-100)
- Feedback generation
- Improvement suggestions

Free tier limits:
- 1500 requests/day
- 1 million tokens/minute
- 10,000 characters/request

## Flutter Changes

### New Screen: GradeExamScreen
- Take photo of answer paper
- Optional: Enter question text
- Submit for AI grading
- Show results with score, feedback, suggestions

### Update ContentScreen
- Add "Grade Exam" button after lesson content
