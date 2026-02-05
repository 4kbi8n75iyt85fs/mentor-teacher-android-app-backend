-- Migration: Add transactions and attendance tables
-- Run this in your Supabase SQL editor

-- Transactions table for cash flow tracking
CREATE TABLE IF NOT EXISTS mentor.transactions (
    id SERIAL PRIMARY KEY,
    date DATE NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('income', 'expense')),
    amount DECIMAL(10, 2) NOT NULL,
    description TEXT,
    category TEXT, -- 'student_fee', 'teacher_salary', 'rent', 'materials', 'other'
    subscription_id INT REFERENCES mentor.subscriptions(id) ON DELETE SET NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Attendance table for GPS proof
CREATE TABLE IF NOT EXISTS mentor.attendance (
    id SERIAL PRIMARY KEY,
    teacher_id TEXT NOT NULL,
    subscription_id INT REFERENCES mentor.subscriptions(id) ON DELETE CASCADE,
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),
    action TEXT NOT NULL CHECK (action IN ('start', 'end')),
    notes TEXT,
    recorded_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_transactions_date ON mentor.transactions(date);
CREATE INDEX IF NOT EXISTS idx_transactions_type ON mentor.transactions(type);
CREATE INDEX IF NOT EXISTS idx_attendance_teacher ON mentor.attendance(teacher_id);
CREATE INDEX IF NOT EXISTS idx_attendance_date ON mentor.attendance(recorded_at);
