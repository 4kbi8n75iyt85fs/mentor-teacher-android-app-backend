package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	godotenv.Load()

	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	_, err = db.Exec("SET search_path TO mentor")
	if err != nil {
		log.Println("Warning: Could not set schema to mentor:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}
	log.Println("Connected to PostgreSQL (mentor schema)")

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		AllowCredentials: true,
	}))

	api := r.Group("/api")
	{
		// Auth
		api.POST("/login", login)
		api.GET("/login", login)

		// Legacy endpoints (for existing app)
		api.GET("/schedule/:teacherId", getSchedule)
		api.GET("/schedule/:teacherId/today", getTodaySchedule)
		api.GET("/students/:teacherId", getStudents)
		api.GET("/subjects/:class", getSubjects)

		// NEW: Subscription-centric endpoints
		api.GET("/subscriptions", getSubscriptions)
		api.GET("/subscriptions/:id", getSubscription)
		api.POST("/subscriptions", createSubscription)
		api.PUT("/subscriptions/:id", updateSubscription)
		api.DELETE("/subscriptions/:id", deleteSubscription)
		api.POST("/subscriptions/:id/complete", markClassComplete)
		api.GET("/subscriptions/:id/progress", getProgress)

		// Teacher CRUD endpoints
		api.GET("/teachers", getTeachers)
		api.GET("/teachers/:id", getTeacher)
		api.POST("/teachers", createTeacher)
		api.PUT("/teachers/:id", updateTeacher)
		api.DELETE("/teachers/:id", deleteTeacher)

		// Teacher's today schedule (V2)
		api.GET("/teacher/:teacherId/today", getTeacherTodayV2)

		// Content Management endpoints
		api.GET("/content", getContentList)
		api.GET("/content/:class/:subject/:chapter", getContent)
		api.POST("/content", upsertContent)
		api.DELETE("/content/:class/:subject/:chapter", deleteContent)

		// Chapters lookup
		api.GET("/chapters", getChapters)

		// Transactions & Analytics endpoints
		api.GET("/transactions", getTransactions)
		api.POST("/transactions", createTransaction)
		api.DELETE("/transactions/:id", deleteTransaction)
		api.GET("/analytics/monthly", getMonthlyAnalytics)

		// Attendance endpoints
		api.POST("/attendance", recordAttendance)
		api.GET("/attendance/:teacherId", getAttendanceHistory)

		// AI Exam Grading endpoints
		api.POST("/exam/submit", submitExamForGrading)
		api.GET("/exam/submissions", getExamSubmissions)
		api.GET("/exam/submissions/:id", getExamSubmission)
		api.PUT("/exam/submissions/:id/review", reviewExamSubmission)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"app":     "Mentor API",
			"version": "2.0.0",
			"status":  "running",
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}
	log.Println("Server starting on port", port)
	r.Run(":" + port)
}

// ============================================
// LOGIN
// ============================================
func login(c *gin.Context) {
	phone := c.Query("phone")
	password := c.Query("password")

	if phone == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Phone and password required"})
		return
	}

	var id, name, teacherPhone string
	var active int

	err := db.QueryRow(
		"SELECT id, name, phone, active FROM mentor.teachers WHERE phone = $1 AND password = $2",
		phone, password,
	).Scan(&id, &name, &teacherPhone, &active)

	if err != nil || active != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Invalid phone or password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"teacher": gin.H{
			"id":    id,
			"name":  name,
			"phone": teacherPhone,
		},
	})
}

// ============================================
// GET ALL SUBSCRIPTIONS (Students)
// ============================================
func getSubscriptions(c *gin.Context) {
	teacherId := c.Query("teacher_id")

	query := `
		SELECT id, student_name, student_phone, guardian_name, guardian_phone,
		       class, subjects, teacher_id, days_per_week, schedule_days, time,
		       amount, billing_date, status, total_classes, completed_classes, progress_percent
		FROM mentor.subscriptions
		WHERE status = 'active'
	`
	args := []interface{}{}

	if teacherId != "" {
		query += " AND teacher_id = $1"
		args = append(args, teacherId)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var subscriptions []gin.H
	for rows.Next() {
		var id, class, daysPerWeek, billingDate, totalClasses, completedClasses int
		var studentName, studentPhone, guardianName, guardianPhone, subjects, teacherID, scheduleDays, schedTime, status string
		var amount, progressPercent float64
		var studentPhoneNull, guardianNameNull, guardianPhoneNull sql.NullString

		rows.Scan(&id, &studentName, &studentPhoneNull, &guardianNameNull, &guardianPhoneNull,
			&class, &subjects, &teacherID, &daysPerWeek, &scheduleDays, &schedTime,
			&amount, &billingDate, &status, &totalClasses, &completedClasses, &progressPercent)

		if studentPhoneNull.Valid {
			studentPhone = studentPhoneNull.String
		}
		if guardianNameNull.Valid {
			guardianName = guardianNameNull.String
		}
		if guardianPhoneNull.Valid {
			guardianPhone = guardianPhoneNull.String
		}

		subscriptions = append(subscriptions, gin.H{
			"id":                id,
			"student_name":      studentName,
			"student_phone":     studentPhone,
			"guardian_name":     guardianName,
			"guardian_phone":    guardianPhone,
			"class":             class,
			"subjects":          strings.Split(subjects, ","),
			"teacher_id":        teacherID,
			"days_per_week":     daysPerWeek,
			"schedule_days":     strings.Split(scheduleDays, ","),
			"time":              schedTime,
			"amount":            amount,
			"billing_date":      billingDate,
			"status":            status,
			"total_classes":     totalClasses,
			"completed_classes": completedClasses,
			"progress_percent":  progressPercent,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "subscriptions": subscriptions})
}

// ============================================
// GET SINGLE SUBSCRIPTION WITH SCHEDULE
// ============================================
func getSubscription(c *gin.Context) {
	id := c.Param("id")

	var subId, class, daysPerWeek, billingDate, totalClasses, completedClasses int
	var studentName, studentPhone, guardianName, guardianPhone, subjects, teacherID, scheduleDays, schedTime, status string
	var amount, progressPercent float64
	var studentPhoneNull, guardianNameNull, guardianPhoneNull sql.NullString

	err := db.QueryRow(`
		SELECT id, student_name, student_phone, guardian_name, guardian_phone,
		       class, subjects, teacher_id, days_per_week, schedule_days, time,
		       amount, billing_date, status, total_classes, completed_classes, progress_percent
		FROM mentor.subscriptions WHERE id = $1
	`, id).Scan(&subId, &studentName, &studentPhoneNull, &guardianNameNull, &guardianPhoneNull,
		&class, &subjects, &teacherID, &daysPerWeek, &scheduleDays, &schedTime,
		&amount, &billingDate, &status, &totalClasses, &completedClasses, &progressPercent)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Subscription not found"})
		return
	}

	if studentPhoneNull.Valid {
		studentPhone = studentPhoneNull.String
	}
	if guardianNameNull.Valid {
		guardianName = guardianNameNull.String
	}
	if guardianPhoneNull.Valid {
		guardianPhone = guardianPhoneNull.String
	}

	// Get schedule (subjects with progress)
	schedRows, _ := db.Query(`
		SELECT id, subject, current_chapter, current_part, total_parts_done, total_parts_needed
		FROM mentor.schedule WHERE subscription_id = $1
	`, id)
	defer schedRows.Close()

	var schedules []gin.H
	for schedRows.Next() {
		var schedId, currentChapter, currentPart, totalPartsDone, totalPartsNeeded int
		var subject string
		schedRows.Scan(&schedId, &subject, &currentChapter, &currentPart, &totalPartsDone, &totalPartsNeeded)

		subjectProgress := float64(0)
		if totalPartsNeeded > 0 {
			subjectProgress = float64(totalPartsDone) / float64(totalPartsNeeded) * 100
		}

		schedules = append(schedules, gin.H{
			"id":                 schedId,
			"subject":            subject,
			"current_chapter":    currentChapter,
			"current_part":       currentPart,
			"total_parts_done":   totalPartsDone,
			"total_parts_needed": totalPartsNeeded,
			"progress_percent":   subjectProgress,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"subscription": gin.H{
			"id":                subId,
			"student_name":      studentName,
			"student_phone":     studentPhone,
			"guardian_name":     guardianName,
			"guardian_phone":    guardianPhone,
			"class":             class,
			"subjects":          strings.Split(subjects, ","),
			"teacher_id":        teacherID,
			"days_per_week":     daysPerWeek,
			"schedule_days":     strings.Split(scheduleDays, ","),
			"time":              schedTime,
			"amount":            amount,
			"billing_date":      billingDate,
			"status":            status,
			"total_classes":     totalClasses,
			"completed_classes": completedClasses,
			"progress_percent":  progressPercent,
			"schedule":          schedules,
		},
	})
}

// ============================================
// CREATE SUBSCRIPTION (Auto-creates schedule)
// ============================================
func createSubscription(c *gin.Context) {
	var input struct {
		StudentName   string  `json:"student_name"`
		StudentPhone  string  `json:"student_phone"`
		GuardianName  string  `json:"guardian_name"`
		GuardianPhone string  `json:"guardian_phone"`
		Class         int     `json:"class"`
		Subjects      string  `json:"subjects"`
		TeacherID     string  `json:"teacher_id"`
		DaysPerWeek   int     `json:"days_per_week"`
		ScheduleDays  string  `json:"schedule_days"`
		Time          string  `json:"time"`
		Amount        float64 `json:"amount"`
		BillingDate   int     `json:"billing_date"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Auto-calculate days_per_week from schedule_days if not provided
	if input.DaysPerWeek == 0 && input.ScheduleDays != "" {
		dayCount := len(strings.Split(input.ScheduleDays, ","))
		input.DaysPerWeek = dayCount
	}

	// Calculate total classes: 1 chapter = 1 class
	subjectList := strings.Split(input.Subjects, ",")
	totalClasses := 0
	var debugInfo []string
	for _, subj := range subjectList {
		subj = strings.TrimSpace(subj)
		var chapters int
		err := db.QueryRow(
			"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND subject = $2",
			input.Class, subj,
		).Scan(&chapters)
		if err != nil {
			// Try case-insensitive search
			err = db.QueryRow(
				"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND LOWER(subject) = LOWER($2)",
				input.Class, subj,
			).Scan(&chapters)
		}
		if err != nil || chapters == 0 {
			debugInfo = append(debugInfo, fmt.Sprintf("NOT_FOUND: class=%d, subject='%s', using default 15", input.Class, subj))
			chapters = 15 // Default if not found
		} else {
			debugInfo = append(debugInfo, fmt.Sprintf("FOUND: class=%d, subject='%s', chapters=%d", input.Class, subj, chapters))
		}
		// Simple formula: 1 chapter = 1 class
		totalClasses += chapters
	}
	log.Printf("CreateSubscription debug: %v, total=%d", debugInfo, totalClasses)

	// Insert subscription
	var subId int
	err := db.QueryRow(`
		INSERT INTO mentor.subscriptions 
		(student_name, student_phone, guardian_name, guardian_phone, class, subjects,
		 teacher_id, days_per_week, schedule_days, time, amount, billing_date, total_classes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`, input.StudentName, input.StudentPhone, input.GuardianName, input.GuardianPhone,
		input.Class, input.Subjects, input.TeacherID, input.DaysPerWeek, input.ScheduleDays,
		input.Time, input.Amount, input.BillingDate, totalClasses).Scan(&subId)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Create schedule entries for each subject
	for _, subj := range subjectList {
		subj = strings.TrimSpace(subj)
		var chapters int
		db.QueryRow(
			"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND subject = $2",
			input.Class, subj,
		).Scan(&chapters)
		if err != nil {
			db.QueryRow(
				"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND LOWER(subject) = LOWER($2)",
				input.Class, subj,
			).Scan(&chapters)
		}
		if chapters == 0 {
			chapters = 15 // Default
		}

		// Simple: 1 chapter = 1 class/part
		db.Exec(`
			INSERT INTO mentor.schedule (subscription_id, subject, total_parts_needed)
			VALUES ($1, $2, $3)
		`, subId, subj, chapters)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"id":            subId,
		"total_classes": totalClasses,
		"debug_info":    debugInfo,
		"message":       "Subscription created with schedule",
	})
}

// ============================================
// UPDATE SUBSCRIPTION
// ============================================
func updateSubscription(c *gin.Context) {
	id := c.Param("id")

	var input struct {
		StudentName   string  `json:"student_name"`
		StudentPhone  string  `json:"student_phone"`
		GuardianName  string  `json:"guardian_name"`
		GuardianPhone string  `json:"guardian_phone"`
		Class         int     `json:"class"`
		Subjects      string  `json:"subjects"`
		TeacherID     string  `json:"teacher_id"`
		ScheduleDays  string  `json:"schedule_days"`
		DaysPerWeek   int     `json:"days_per_week"`
		Time          string  `json:"time"`
		Amount        float64 `json:"amount"`
		Status        string  `json:"status"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Auto-calculate days_per_week from schedule_days
	daysPerWeek := input.DaysPerWeek
	if daysPerWeek == 0 && input.ScheduleDays != "" {
		daysPerWeek = len(strings.Split(input.ScheduleDays, ","))
	}

	// Recalculate total_classes based on new subjects
	totalClasses := 0
	if input.Class > 0 && input.Subjects != "" {
		subjectList := strings.Split(input.Subjects, ",")
		for _, subj := range subjectList {
			subj = strings.TrimSpace(subj)
			var chapters int
			err := db.QueryRow(
				"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND subject = $2",
				input.Class, subj,
			).Scan(&chapters)
			if err != nil {
				// Try case-insensitive search
				db.QueryRow(
					"SELECT total_chapters FROM mentor.chapters WHERE class = $1 AND LOWER(subject) = LOWER($2)",
					input.Class, subj,
				).Scan(&chapters)
			}
			if chapters == 0 {
				chapters = 15 // Default if not found
			}
			totalClasses += chapters
		}
	}

	_, err := db.Exec(`
		UPDATE mentor.subscriptions SET 
			student_name = $1, student_phone = $2, guardian_name = $3, guardian_phone = $4,
			class = $5, subjects = $6, teacher_id = $7, schedule_days = $8, time = $9,
			amount = $10, status = COALESCE(NULLIF($11, ''), 'active'), days_per_week = $12, 
			total_classes = $13, updated_at = NOW()
		WHERE id = $14
	`, input.StudentName, input.StudentPhone, input.GuardianName, input.GuardianPhone,
		input.Class, input.Subjects, input.TeacherID, input.ScheduleDays, input.Time,
		input.Amount, input.Status, daysPerWeek, totalClasses, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Subscription updated", "total_classes": totalClasses})
}

// ============================================
// DELETE SUBSCRIPTION
// ============================================
func deleteSubscription(c *gin.Context) {
	id := c.Param("id")

	// Delete related records first
	db.Exec("DELETE FROM mentor.progress WHERE subscription_id = $1", id)
	db.Exec("DELETE FROM mentor.schedule WHERE subscription_id = $1", id)

	_, err := db.Exec("DELETE FROM mentor.subscriptions WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Subscription deleted"})
}

// ============================================
// MARK CLASS COMPLETE (Updates progress)
// ============================================
func markClassComplete(c *gin.Context) {
	subId := c.Param("id")

	var input struct {
		ScheduleID int    `json:"schedule_id"`
		Subject    string `json:"subject"`
		TeacherID  string `json:"teacher_id"`
		Notes      string `json:"notes"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Get current chapter/part from schedule
	var schedId, currentChapter, currentPart, totalPartsDone, totalPartsNeeded int
	err := db.QueryRow(`
		SELECT id, current_chapter, current_part, total_parts_done, total_parts_needed
		FROM mentor.schedule WHERE subscription_id = $1 AND subject = $2
	`, subId, input.Subject).Scan(&schedId, &currentChapter, &currentPart, &totalPartsDone, &totalPartsNeeded)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Schedule not found"})
		return
	}

	// Add progress record
	db.Exec(`
		INSERT INTO mentor.progress (subscription_id, schedule_id, subject, chapter, part, teacher_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, subId, schedId, input.Subject, currentChapter, currentPart, input.TeacherID, input.Notes)

	// Advance to next part/chapter
	newPart := currentPart + 1
	newChapter := currentChapter
	if newPart > 3 {
		newPart = 1
		newChapter++
	}
	totalPartsDone++

	// Update schedule
	db.Exec(`
		UPDATE mentor.schedule 
		SET current_chapter = $1, current_part = $2, total_parts_done = $3
		WHERE id = $4
	`, newChapter, newPart, totalPartsDone, schedId)

	// Update subscription totals
	var totalCompleted int
	db.QueryRow(`
		SELECT COALESCE(SUM(total_parts_done), 0) FROM mentor.schedule WHERE subscription_id = $1
	`, subId).Scan(&totalCompleted)

	var totalNeeded int
	db.QueryRow(`SELECT total_classes FROM mentor.subscriptions WHERE id = $1`, subId).Scan(&totalNeeded)

	progressPercent := float64(0)
	if totalNeeded > 0 {
		progressPercent = float64(totalCompleted) / float64(totalNeeded) * 100
	}

	db.Exec(`
		UPDATE mentor.subscriptions 
		SET completed_classes = $1, progress_percent = $2, updated_at = NOW()
		WHERE id = $3
	`, totalCompleted, progressPercent, subId)

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"new_chapter":      newChapter,
		"new_part":         newPart,
		"completed_total":  totalCompleted,
		"progress_percent": progressPercent,
		"message":          "Class marked as complete",
	})
}

// ============================================
// GET PROGRESS HISTORY
// ============================================
func getProgress(c *gin.Context) {
	subId := c.Param("id")

	rows, err := db.Query(`
		SELECT id, subject, chapter, part, teacher_id, notes, completed_at
		FROM mentor.progress WHERE subscription_id = $1
		ORDER BY completed_at DESC LIMIT 50
	`, subId)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var progress []gin.H
	for rows.Next() {
		var id, chapter, part int
		var subject, teacherId, notes string
		var completedAt time.Time
		var notesNull, teacherIdNull sql.NullString

		rows.Scan(&id, &subject, &chapter, &part, &teacherIdNull, &notesNull, &completedAt)

		if notesNull.Valid {
			notes = notesNull.String
		}
		if teacherIdNull.Valid {
			teacherId = teacherIdNull.String
		}

		progress = append(progress, gin.H{
			"id":           id,
			"subject":      subject,
			"chapter":      chapter,
			"part":         part,
			"teacher_id":   teacherId,
			"notes":        notes,
			"completed_at": completedAt.Format("2006-01-02 15:04"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "progress": progress})
}

// ============================================
// GET TEACHER'S TODAY SCHEDULE (V2)
// ============================================
func getTeacherTodayV2(c *gin.Context) {
	teacherId := c.Param("teacherId")
	todayName := getDayName() // "Mon", "Tue", etc.

	// Map day names to codes: Sun=2, Mon=3, Tue=4, Wed=5, Thu=6, Fri=7, Sat=1
	dayNameToCode := map[string]string{
		"Sat": "1", "Sun": "2", "Mon": "3", "Tue": "4",
		"Wed": "5", "Thu": "6", "Fri": "7",
	}
	todayCode := dayNameToCode[todayName]

	// Query for students where schedule_days contains either the day name OR day code
	rows, err := db.Query(`
		SELECT s.id, s.student_name, s.class, s.subjects, s.schedule_days, s.time,
		       s.completed_classes, s.total_classes, s.progress_percent
		FROM mentor.subscriptions s
		WHERE s.teacher_id = $1 AND s.status = 'active' 
		  AND (s.schedule_days LIKE $2 OR s.schedule_days LIKE $3)
		ORDER BY s.time
	`, teacherId, "%"+todayName+"%", "%"+todayCode+"%")

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var sessions []gin.H
	for rows.Next() {
		var id, class, completedClasses, totalClasses int
		var studentName, subjects, scheduleDays, schedTime string
		var progressPercent float64

		rows.Scan(&id, &studentName, &class, &subjects, &scheduleDays, &schedTime,
			&completedClasses, &totalClasses, &progressPercent)

		// Get current subject progress
		schedRows, _ := db.Query(`
			SELECT subject, current_chapter, current_part FROM mentor.schedule WHERE subscription_id = $1
		`, id)

		var subjectProgress []gin.H
		for schedRows.Next() {
			var subj string
			var ch, pt int
			schedRows.Scan(&subj, &ch, &pt)
			subjectProgress = append(subjectProgress, gin.H{
				"subject":         subj,
				"current_chapter": ch,
				"current_part":    pt,
			})
		}
		schedRows.Close()

		sessions = append(sessions, gin.H{
			"subscription_id":   id,
			"student_name":      studentName,
			"class":             class,
			"subjects":          strings.Split(subjects, ","),
			"schedule_days":     strings.Split(scheduleDays, ","),
			"time":              schedTime,
			"completed_classes": completedClasses,
			"total_classes":     totalClasses,
			"progress_percent":  progressPercent,
			"subject_progress":  subjectProgress,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"today":      todayName,
		"today_code": todayCode,
		"sessions":   sessions,
	})
}

// ============================================
// LEGACY ENDPOINTS (Keep existing app working)
// ============================================
func getSchedule(c *gin.Context) {
	teacherId := c.Param("teacherId")

	rows, err := db.Query(`
		SELECT s.id, s.student_name, s.class, s.subjects, s.schedule_days, s.time,
		       s.completed_classes, s.total_classes, s.progress_percent
		FROM mentor.subscriptions s
		WHERE s.teacher_id = $1 AND s.status = 'active'
	`, teacherId)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var schedules []gin.H
	for rows.Next() {
		var id, class, completedClasses, totalClasses int
		var studentName, subjects, scheduleDays, schedTime string
		var progressPercent float64

		rows.Scan(&id, &studentName, &class, &subjects, &scheduleDays, &schedTime,
			&completedClasses, &totalClasses, &progressPercent)

		schedules = append(schedules, gin.H{
			"id": strconv.Itoa(id),
			"student": gin.H{
				"id":    strconv.Itoa(id),
				"name":  studentName,
				"class": class,
			},
			"subject":          strings.Split(subjects, ",")[0],
			"class":            class,
			"days":             strings.Split(scheduleDays, ","),
			"time":             schedTime,
			"current_chapter":  1,
			"current_part":     1,
			"progress_percent": progressPercent,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "schedules": schedules})
}

func getTodaySchedule(c *gin.Context) {
	teacherId := c.Param("teacherId")
	todayName := getDayName()

	// Map day names to codes: Sun=2, Mon=3, Tue=4, Wed=5, Thu=6, Fri=7, Sat=1
	dayNameToCode := map[string]string{
		"Sat": "1", "Sun": "2", "Mon": "3", "Tue": "4",
		"Wed": "5", "Thu": "6", "Fri": "7",
	}
	todayCode := dayNameToCode[todayName]

	// Check for holiday
	var holidayName string
	todayDate := time.Now().Format("2006-01-02")
	err := db.QueryRow("SELECT name FROM mentor.holidays WHERE date = $1", todayDate).Scan(&holidayName)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"schedules":   []gin.H{},
			"isHoliday":   true,
			"holidayName": holidayName,
		})
		return
	}

	// Query matching both day name (Mon) and day code (3)
	rows, _ := db.Query(`
		SELECT s.id, s.student_name, s.class, s.subjects, s.schedule_days, s.time,
		       s.total_classes, s.completed_classes, s.progress_percent,
		       COALESCE(s.schedule_json::TEXT, '{}')
		FROM mentor.subscriptions s
		WHERE s.teacher_id = $1 AND s.status = 'active' 
		  AND (s.schedule_days LIKE $2 OR s.schedule_days LIKE $3)
	`, teacherId, "%"+todayName+"%", "%"+todayCode+"%")
	defer rows.Close()

	var schedules []gin.H
	for rows.Next() {
		var id, class, totalClasses, completedClasses int
		var studentName, subjects, scheduleDays, schedTime, scheduleJSON string
		var progressPercent float64

		rows.Scan(&id, &studentName, &class, &subjects, &scheduleDays, &schedTime,
			&totalClasses, &completedClasses, &progressPercent, &scheduleJSON)

		// Find today's class from schedule_json
		var currentChapter, currentPart int = 1, 1
		var todaySubject string

		// Parse schedule_json to find today's lesson
		// For now, use first subject and get from schedule table
		db.QueryRow(`
			SELECT current_chapter, current_part FROM mentor.schedule 
			WHERE subscription_id = $1 LIMIT 1
		`, id).Scan(&currentChapter, &currentPart)

		// Use first subject if todaySubject not set
		if todaySubject == "" {
			subjectList := strings.Split(subjects, ",")
			if len(subjectList) > 0 {
				todaySubject = strings.TrimSpace(subjectList[0])
			}
		}

		schedules = append(schedules, gin.H{
			"id": strconv.Itoa(id),
			"student": gin.H{
				"id":    strconv.Itoa(id),
				"name":  studentName,
				"class": class,
			},
			"subscription_id":   id,
			"student_name":      studentName,
			"subject":           todaySubject,
			"subjects":          strings.Split(subjects, ","),
			"class":             class,
			"days":              strings.Split(scheduleDays, ","),
			"time":              schedTime,
			"current_chapter":   currentChapter,
			"current_part":      currentPart,
			"total_classes":     totalClasses,
			"completed_classes": completedClasses,
			"progress_percent":  progressPercent,
			"schedule_json":     scheduleJSON,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "schedules": schedules, "today": todayName})
}

func getStudents(c *gin.Context) {
	teacherId := c.Param("teacherId")

	rows, _ := db.Query(`
		SELECT id, student_name, class, subjects, time FROM mentor.subscriptions
		WHERE teacher_id = $1 AND status = 'active'
	`, teacherId)
	defer rows.Close()

	var students []gin.H
	for rows.Next() {
		var id, class int
		var name, subjects, studentTime string
		rows.Scan(&id, &name, &class, &subjects, &studentTime)

		students = append(students, gin.H{
			"id":       strconv.Itoa(id),
			"name":     name,
			"class":    class,
			"subjects": strings.Split(subjects, ","),
			"time":     studentTime,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "students": students})
}

func getSubjects(c *gin.Context) {
	classNum := c.Param("class")

	rows, _ := db.Query("SELECT DISTINCT subject FROM mentor.chapters WHERE class = $1", classNum)
	defer rows.Close()

	var subjects []string
	for rows.Next() {
		var subj string
		rows.Scan(&subj)
		subjects = append(subjects, subj)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "subjects": subjects})
}

func getDayName() string {
	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	return days[time.Now().Weekday()]
}

// ============================================
// TEACHER CRUD FUNCTIONS
// ============================================

func getTeachers(c *gin.Context) {
	rows, err := db.Query(`
		SELECT id, name, phone, password 
		FROM mentor.teachers 
		ORDER BY id
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var teachers []gin.H
	for rows.Next() {
		var id, name, phone, password string
		if err := rows.Scan(&id, &name, &phone, &password); err != nil {
			continue
		}
		teachers = append(teachers, gin.H{
			"id":       id,
			"name":     name,
			"phone":    phone,
			"password": password,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "teachers": teachers})
}

func getTeacher(c *gin.Context) {
	id := c.Param("id")

	var name, phone, password string
	err := db.QueryRow(`
		SELECT name, phone, password 
		FROM mentor.teachers WHERE id = $1
	`, id).Scan(&name, &phone, &password)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Teacher not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"teacher": gin.H{
			"id":       id,
			"name":     name,
			"phone":    phone,
			"password": password,
		},
	})
}

func createTeacher(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-generate teacher ID starting from 1001
	var maxID int
	db.QueryRow(`SELECT COALESCE(MAX(CAST(id AS INTEGER)), 1000) FROM mentor.teachers WHERE id ~ '^[0-9]+$'`).Scan(&maxID)
	newID := strconv.Itoa(maxID + 1)

	_, err := db.Exec(`
		INSERT INTO mentor.teachers (id, name, phone, password)
		VALUES ($1, $2, $3, $4)
	`, newID, req.Name, req.Phone, req.Password)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": newID, "message": "Teacher created"})
}

func updateTeacher(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec(`
		UPDATE mentor.teachers 
		SET name = $1, phone = $2, password = $3
		WHERE id = $4
	`, req.Name, req.Phone, req.Password, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Teacher updated"})
}

func deleteTeacher(c *gin.Context) {
	id := c.Param("id")

	_, err := db.Exec(`DELETE FROM mentor.teachers WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Teacher deleted"})
}

// ============================================
// CONTENT MANAGEMENT
// ============================================

func getChapters(c *gin.Context) {
	classNum := c.Query("class")

	var rows *sql.Rows
	var err error

	if classNum != "" {
		rows, err = db.Query(`
			SELECT class, subject, total_chapters
			FROM mentor.chapters WHERE class = $1
			ORDER BY subject
		`, classNum)
	} else {
		rows, err = db.Query(`
			SELECT class, subject, total_chapters
			FROM mentor.chapters
			ORDER BY class, subject
		`)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var chapters []gin.H
	for rows.Next() {
		var class, totalChapters int
		var subject string
		rows.Scan(&class, &subject, &totalChapters)
		chapters = append(chapters, gin.H{
			"class":          class,
			"subject":        subject,
			"total_chapters": totalChapters,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "chapters": chapters})
}

func getContentList(c *gin.Context) {
	classNum := c.Query("class")
	subject := c.Query("subject")

	query := `SELECT id, class, subject, chapter_number, chapter_title, created_at, updated_at
			  FROM mentor.content WHERE 1=1`
	args := []interface{}{}
	argCount := 0

	if classNum != "" {
		argCount++
		query += fmt.Sprintf(" AND class = $%d", argCount)
		args = append(args, classNum)
	}
	if subject != "" {
		argCount++
		query += fmt.Sprintf(" AND subject = $%d", argCount)
		args = append(args, subject)
	}
	query += " ORDER BY class, subject, chapter_number"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var content []gin.H
	for rows.Next() {
		var id, class, chapterNum int
		var subject, chapterTitle string
		var createdAt, updatedAt time.Time
		var chapterTitleNull sql.NullString

		rows.Scan(&id, &class, &subject, &chapterNum, &chapterTitleNull, &createdAt, &updatedAt)

		if chapterTitleNull.Valid {
			chapterTitle = chapterTitleNull.String
		}

		content = append(content, gin.H{
			"id":             id,
			"class":          class,
			"subject":        subject,
			"chapter_number": chapterNum,
			"chapter_title":  chapterTitle,
			"created_at":     createdAt.Format("2006-01-02 15:04"),
			"updated_at":     updatedAt.Format("2006-01-02 15:04"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "content": content})
}

func getContent(c *gin.Context) {
	classNum := c.Param("class")
	subject := c.Param("subject")
	chapter := c.Param("chapter")

	var id, class, chapterNum int
	var subjectName, chapterTitle string
	var contentJSON string
	var chapterTitleNull sql.NullString

	err := db.QueryRow(`
		SELECT id, class, subject, chapter_number, chapter_title, content_json::text
		FROM mentor.content
		WHERE class = $1 AND subject = $2 AND chapter_number = $3
	`, classNum, subject, chapter).Scan(&id, &class, &subjectName, &chapterNum, &chapterTitleNull, &contentJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusOK, gin.H{"success": true, "content": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if chapterTitleNull.Valid {
		chapterTitle = chapterTitleNull.String
	}

	// Parse the content_json string into actual JSON
	var parsedContent map[string]interface{}
	if err := json.Unmarshal([]byte(contentJSON), &parsedContent); err != nil {
		// If parsing fails, return empty sections
		parsedContent = map[string]interface{}{"sections": []interface{}{}}
	}

	// Merge parsed content with metadata
	parsedContent["id"] = id
	parsedContent["class"] = class
	parsedContent["subject"] = subjectName
	parsedContent["chapter_number"] = chapterNum
	parsedContent["chapter_title"] = chapterTitle

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"content": parsedContent,
	})
}

func upsertContent(c *gin.Context) {
	var input struct {
		Class         int         `json:"class"`
		Subject       string      `json:"subject"`
		ChapterNumber int         `json:"chapter_number"`
		ChapterTitle  string      `json:"chapter_title"`
		ContentJSON   interface{} `json:"content_json"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert content to JSON string
	contentBytes, err := json.Marshal(input.ContentJSON)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid content JSON"})
		return
	}

	// Upsert (insert or update on conflict)
	_, err = db.Exec(`
		INSERT INTO mentor.content (class, subject, chapter_number, chapter_title, content_json)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (class, subject, chapter_number) 
		DO UPDATE SET 
			chapter_title = EXCLUDED.chapter_title,
			content_json = EXCLUDED.content_json,
			updated_at = NOW()
	`, input.Class, input.Subject, input.ChapterNumber, input.ChapterTitle, string(contentBytes))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Content saved"})
}

func deleteContent(c *gin.Context) {
	classNum := c.Param("class")
	subject := c.Param("subject")
	chapter := c.Param("chapter")

	_, err := db.Exec(`
		DELETE FROM mentor.content 
		WHERE class = $1 AND subject = $2 AND chapter_number = $3
	`, classNum, subject, chapter)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Content deleted"})
}

// ============================================
// TRANSACTIONS (Cash Flow)
// ============================================
func getTransactions(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")

	query := `
		SELECT id, date, type, amount, description, category, subscription_id, created_at
		FROM mentor.transactions
		WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if year != "" && month != "" {
		query += fmt.Sprintf(" AND EXTRACT(YEAR FROM date) = $%d AND EXTRACT(MONTH FROM date) = $%d", argNum, argNum+1)
		args = append(args, year, month)
		argNum += 2
	}

	query += " ORDER BY date DESC, created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var transactions []gin.H
	for rows.Next() {
		var id int
		var date, txType, description, category string
		var amount float64
		var subscriptionId sql.NullInt64
		var createdAt time.Time
		var categoryNull, descNull sql.NullString

		rows.Scan(&id, &date, &txType, &amount, &descNull, &categoryNull, &subscriptionId, &createdAt)

		if descNull.Valid {
			description = descNull.String
		}
		if categoryNull.Valid {
			category = categoryNull.String
		}

		tx := gin.H{
			"id":          id,
			"date":        date,
			"type":        txType,
			"amount":      amount,
			"description": description,
			"category":    category,
			"created_at":  createdAt.Format("2006-01-02 15:04"),
		}
		if subscriptionId.Valid {
			tx["subscription_id"] = subscriptionId.Int64
		}
		transactions = append(transactions, tx)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "transactions": transactions})
}

func createTransaction(c *gin.Context) {
	var input struct {
		Date           string  `json:"date"`
		Type           string  `json:"type"` // "income" or "expense"
		Amount         float64 `json:"amount"`
		Description    string  `json:"description"`
		Category       string  `json:"category"` // "student_fee", "teacher_salary", "rent", "materials", "other"
		SubscriptionID *int    `json:"subscription_id"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if input.Date == "" || input.Type == "" || input.Amount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "date, type, and amount are required"})
		return
	}

	var id int
	err := db.QueryRow(`
		INSERT INTO mentor.transactions (date, type, amount, description, category, subscription_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, input.Date, input.Type, input.Amount, input.Description, input.Category, input.SubscriptionID).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": id, "message": "Transaction created"})
}

func deleteTransaction(c *gin.Context) {
	id := c.Param("id")

	_, err := db.Exec("DELETE FROM mentor.transactions WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Transaction deleted"})
}

func getMonthlyAnalytics(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")

	if year == "" || month == "" {
		now := time.Now()
		year = strconv.Itoa(now.Year())
		month = strconv.Itoa(int(now.Month()))
	}

	// Get total income
	var totalIncome float64
	db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM mentor.transactions 
		WHERE type = 'income' AND EXTRACT(YEAR FROM date) = $1 AND EXTRACT(MONTH FROM date) = $2
	`, year, month).Scan(&totalIncome)

	// Get total expenses
	var totalExpenses float64
	db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM mentor.transactions 
		WHERE type = 'expense' AND EXTRACT(YEAR FROM date) = $1 AND EXTRACT(MONTH FROM date) = $2
	`, year, month).Scan(&totalExpenses)

	// Get breakdown by category
	categoryRows, _ := db.Query(`
		SELECT category, type, SUM(amount) as total
		FROM mentor.transactions 
		WHERE EXTRACT(YEAR FROM date) = $1 AND EXTRACT(MONTH FROM date) = $2
		GROUP BY category, type
		ORDER BY total DESC
	`, year, month)
	defer categoryRows.Close()

	var categoryBreakdown []gin.H
	for categoryRows.Next() {
		var category, txType string
		var total float64
		var catNull sql.NullString
		categoryRows.Scan(&catNull, &txType, &total)
		if catNull.Valid {
			category = catNull.String
		} else {
			category = "uncategorized"
		}
		categoryBreakdown = append(categoryBreakdown, gin.H{
			"category": category,
			"type":     txType,
			"total":    total,
		})
	}

	// Get daily breakdown for calendar view
	dailyRows, _ := db.Query(`
		SELECT date, type, SUM(amount) as total
		FROM mentor.transactions 
		WHERE EXTRACT(YEAR FROM date) = $1 AND EXTRACT(MONTH FROM date) = $2
		GROUP BY date, type
		ORDER BY date
	`, year, month)
	defer dailyRows.Close()

	dailyData := make(map[string]gin.H)
	for dailyRows.Next() {
		var date, txType string
		var total float64
		dailyRows.Scan(&date, &txType, &total)

		if _, exists := dailyData[date]; !exists {
			dailyData[date] = gin.H{"date": date, "income": 0.0, "expense": 0.0}
		}
		dailyData[date][txType] = total
	}

	var dailyList []gin.H
	for _, v := range dailyData {
		dailyList = append(dailyList, v)
	}

	// Get student count and active subscriptions
	var activeStudents int
	db.QueryRow("SELECT COUNT(*) FROM mentor.subscriptions WHERE status = 'active'").Scan(&activeStudents)

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"year":          year,
		"month":         month,
		"total_income":  totalIncome,
		"total_expense": totalExpenses,
		"profit":        totalIncome - totalExpenses,
		"categories":    categoryBreakdown,
		"daily":         dailyList,
		"active_students": activeStudents,
	})
}

// ============================================
// ATTENDANCE (GPS Proof)
// ============================================
func recordAttendance(c *gin.Context) {
	var input struct {
		TeacherID      string  `json:"teacher_id"`
		SubscriptionID int     `json:"subscription_id"`
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		Action         string  `json:"action"` // "start" or "end"
		Notes          string  `json:"notes"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	var id int
	err := db.QueryRow(`
		INSERT INTO mentor.attendance (teacher_id, subscription_id, latitude, longitude, action, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, input.TeacherID, input.SubscriptionID, input.Latitude, input.Longitude, input.Action, input.Notes).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"id":        id,
		"message":   "Attendance recorded",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func getAttendanceHistory(c *gin.Context) {
	teacherId := c.Param("teacherId")
	dateFrom := c.Query("from")
	dateTo := c.Query("to")

	query := `
		SELECT a.id, a.subscription_id, s.student_name, a.latitude, a.longitude, 
		       a.action, a.notes, a.recorded_at
		FROM mentor.attendance a
		LEFT JOIN mentor.subscriptions s ON a.subscription_id = s.id
		WHERE a.teacher_id = $1
	`
	args := []interface{}{teacherId}

	if dateFrom != "" {
		query += " AND DATE(a.recorded_at) >= $2"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		query += fmt.Sprintf(" AND DATE(a.recorded_at) <= $%d", len(args)+1)
		args = append(args, dateTo)
	}

	query += " ORDER BY a.recorded_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	var records []gin.H
	for rows.Next() {
		var id, subscriptionId int
		var studentName, action, notes string
		var latitude, longitude float64
		var recordedAt time.Time
		var studentNameNull, notesNull sql.NullString

		rows.Scan(&id, &subscriptionId, &studentNameNull, &latitude, &longitude, &action, &notesNull, &recordedAt)

		if studentNameNull.Valid {
			studentName = studentNameNull.String
		}
		if notesNull.Valid {
			notes = notesNull.String
		}

		records = append(records, gin.H{
			"id":              id,
			"subscription_id": subscriptionId,
			"student_name":    studentName,
			"latitude":        latitude,
			"longitude":       longitude,
			"action":          action,
			"notes":           notes,
			"recorded_at":     recordedAt.Format("2006-01-02 15:04"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "attendance": records})
}

// ============================================
// AI EXAM GRADING (Gemini 1.5 Flash)
// ============================================

var geminiAPIKey = os.Getenv("GEMINI_API_KEY")

func submitExamForGrading(c *gin.Context) {
	var input struct {
		SubscriptionID int    `json:"subscription_id"`
		TeacherID      string `json:"teacher_id"`
		StudentName    string `json:"student_name"`
		Class          int    `json:"class"`
		Subject        string `json:"subject"`
		ChapterNumber  int    `json:"chapter_number"`
		QuestionText   string `json:"question_text"`
		ImageBase64    string `json:"image_base64"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.ImageBase64 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image is required"})
		return
	}

	// Call Gemini API for AI grading
	score, feedback, suggestions, err := gradeWithGemini(input.QuestionText, input.ImageBase64, input.Subject)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI grading failed: " + err.Error()})
		return
	}

	// Store in database
	var submissionID int
	err = db.QueryRow(`
		INSERT INTO mentor.exam_submissions 
		(subscription_id, teacher_id, student_name, class, subject, chapter_number, question_text, image_data, ai_score, ai_feedback, ai_suggestions, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'graded')
		RETURNING id
	`, input.SubscriptionID, input.TeacherID, input.StudentName, input.Class, input.Subject, 
	   input.ChapterNumber, input.QuestionText, input.ImageBase64, score, feedback, suggestions).Scan(&submissionID)

	if err != nil {
		// Table might not exist, create it
		_, createErr := db.Exec(`
			CREATE TABLE IF NOT EXISTS mentor.exam_submissions (
				id SERIAL PRIMARY KEY,
				subscription_id INTEGER,
				teacher_id VARCHAR(50) NOT NULL,
				student_name VARCHAR(255) NOT NULL,
				class INTEGER NOT NULL,
				subject VARCHAR(255) NOT NULL,
				chapter_number INTEGER,
				question_text TEXT,
				image_data TEXT,
				ai_score INTEGER,
				ai_feedback TEXT,
				ai_suggestions TEXT,
				teacher_notes TEXT,
				status VARCHAR(50) DEFAULT 'pending',
				created_at TIMESTAMP DEFAULT NOW(),
				updated_at TIMESTAMP DEFAULT NOW()
			)
		`)
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		// Retry insert
		err = db.QueryRow(`
			INSERT INTO mentor.exam_submissions 
			(subscription_id, teacher_id, student_name, class, subject, chapter_number, question_text, image_data, ai_score, ai_feedback, ai_suggestions, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'graded')
			RETURNING id
		`, input.SubscriptionID, input.TeacherID, input.StudentName, input.Class, input.Subject, 
		   input.ChapterNumber, input.QuestionText, input.ImageBase64, score, feedback, suggestions).Scan(&submissionID)
		
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"submission_id": submissionID,
		"score":         score,
		"feedback":      feedback,
		"suggestions":   suggestions,
	})
}

func gradeWithGemini(questionText, imageBase64, subject string) (int, string, string, error) {
	if geminiAPIKey == "" {
		return 0, "", "", fmt.Errorf("GEMINI_API_KEY not configured")
	}

	// Prepare prompt for grading
	prompt := fmt.Sprintf(`You are an expert teacher grading a student's answer paper for the subject: %s

Please analyze this handwritten answer paper image and provide:
1. A score from 0 to 100
2. Feedback on what the student did well
3. Specific suggestions for improvement

%s

IMPORTANT: Respond in this exact JSON format:
{
  "score": <number 0-100>,
  "feedback": "<what was done well>",
  "suggestions": "<specific improvement suggestions>"
}`, subject, func() string {
		if questionText != "" {
			return fmt.Sprintf("The question being answered was: %s", questionText)
		}
		return "No specific question was provided, evaluate the answer based on the content visible."
	}())

	// Call Gemini API
	apiURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=" + geminiAPIKey

	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
					{
						"inline_data": map[string]string{
							"mime_type": "image/jpeg",
							"data":      imageBase64,
						},
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.2,
			"maxOutputTokens": 1024,
		},
	}

	jsonBody, _ := json.Marshal(requestBody)
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return 0, "", "", fmt.Errorf("Gemini API error: %s", string(body))
	}

	// Parse Gemini response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	json.Unmarshal(body, &geminiResp)

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return 0, "", "", fmt.Errorf("empty response from Gemini")
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Extract JSON from response (handle markdown code blocks)
	jsonStart := strings.Index(responseText, "{")
	jsonEnd := strings.LastIndex(responseText, "}")
	if jsonStart == -1 || jsonEnd == -1 {
		return 0, "", "", fmt.Errorf("could not parse Gemini response")
	}
	jsonStr := responseText[jsonStart : jsonEnd+1]

	var gradeResult struct {
		Score       int    `json:"score"`
		Feedback    string `json:"feedback"`
		Suggestions string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &gradeResult); err != nil {
		return 0, "", "", fmt.Errorf("invalid JSON from Gemini: %s", jsonStr)
	}

	return gradeResult.Score, gradeResult.Feedback, gradeResult.Suggestions, nil
}

func getExamSubmissions(c *gin.Context) {
	teacherID := c.Query("teacher_id")
	studentName := c.Query("student_name")

	query := `
		SELECT id, subscription_id, teacher_id, student_name, class, subject, chapter_number, 
		       ai_score, ai_feedback, ai_suggestions, teacher_notes, status, created_at
		FROM mentor.exam_submissions
		WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if teacherID != "" {
		query += fmt.Sprintf(" AND teacher_id = $%d", argNum)
		args = append(args, teacherID)
		argNum++
	}
	if studentName != "" {
		query += fmt.Sprintf(" AND student_name ILIKE $%d", argNum)
		args = append(args, "%"+studentName+"%")
		argNum++
	}

	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "submissions": []interface{}{}})
		return
	}
	defer rows.Close()

	var submissions []gin.H
	for rows.Next() {
		var id, subscriptionID, class, chapterNumber int
		var aiScore sql.NullInt64
		var teacherID, studentName, subject, status string
		var aiFeedback, aiSuggestions, teacherNotes sql.NullString
		var createdAt time.Time

		rows.Scan(&id, &subscriptionID, &teacherID, &studentName, &class, &subject, &chapterNumber,
			&aiScore, &aiFeedback, &aiSuggestions, &teacherNotes, &status, &createdAt)

		submissions = append(submissions, gin.H{
			"id":              id,
			"subscription_id": subscriptionID,
			"teacher_id":      teacherID,
			"student_name":    studentName,
			"class":           class,
			"subject":         subject,
			"chapter_number":  chapterNumber,
			"ai_score":        aiScore.Int64,
			"ai_feedback":     aiFeedback.String,
			"ai_suggestions":  aiSuggestions.String,
			"teacher_notes":   teacherNotes.String,
			"status":          status,
			"created_at":      createdAt.Format("2006-01-02 15:04"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "submissions": submissions})
}

func getExamSubmission(c *gin.Context) {
	id := c.Param("id")

	var submissionID, subscriptionID, class, chapterNumber int
	var aiScore sql.NullInt64
	var teacherID, studentName, subject, status string
	var questionText, imageData, aiFeedback, aiSuggestions, teacherNotes sql.NullString
	var createdAt time.Time

	err := db.QueryRow(`
		SELECT id, subscription_id, teacher_id, student_name, class, subject, chapter_number,
		       question_text, image_data, ai_score, ai_feedback, ai_suggestions, teacher_notes, status, created_at
		FROM mentor.exam_submissions WHERE id = $1
	`, id).Scan(&submissionID, &subscriptionID, &teacherID, &studentName, &class, &subject, &chapterNumber,
		&questionText, &imageData, &aiScore, &aiFeedback, &aiSuggestions, &teacherNotes, &status, &createdAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Submission not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"submission": gin.H{
			"id":              submissionID,
			"subscription_id": subscriptionID,
			"teacher_id":      teacherID,
			"student_name":    studentName,
			"class":           class,
			"subject":         subject,
			"chapter_number":  chapterNumber,
			"question_text":   questionText.String,
			"image_data":      imageData.String,
			"ai_score":        aiScore.Int64,
			"ai_feedback":     aiFeedback.String,
			"ai_suggestions":  aiSuggestions.String,
			"teacher_notes":   teacherNotes.String,
			"status":          status,
			"created_at":      createdAt.Format("2006-01-02 15:04"),
		},
	})
}

func reviewExamSubmission(c *gin.Context) {
	id := c.Param("id")

	var input struct {
		TeacherNotes string `json:"teacher_notes"`
		FinalScore   *int   `json:"final_score"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	query := "UPDATE mentor.exam_submissions SET teacher_notes = $1, status = 'reviewed', updated_at = NOW()"
	args := []interface{}{input.TeacherNotes}
	
	if input.FinalScore != nil {
		query += ", ai_score = $2 WHERE id = $3"
		args = append(args, *input.FinalScore, id)
	} else {
		query += " WHERE id = $2"
		args = append(args, id)
	}

	_, err := db.Exec(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Submission reviewed"})
}
