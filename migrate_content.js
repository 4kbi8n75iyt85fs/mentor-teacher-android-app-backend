#!/usr/bin/env node

/**
 * Content Migration Script
 * Migrates JSON chapter files from class-content folder to Supabase database
 * 
 * Usage: node migrate_content.js
 */

const fs = require('fs');
const path = require('path');
const https = require('https');

// Configuration
const API_URL = 'https://spare-ardisj-665ghh6r-673c35eb.koyeb.app/api';
const CONTENT_DIR = path.join(__dirname, '..', 'class-content');

// Class folder to class number mapping
function getClassNumber(folderName) {
    const match = folderName.match(/c(\d+)/);
    return match ? parseInt(match[1]) : null;
}

// Read all content files
function getAllContentFiles() {
    const files = [];

    // Read class folders (c1, c2, ..., c12)
    const classFolders = fs.readdirSync(CONTENT_DIR).filter(f =>
        f.startsWith('c') && fs.statSync(path.join(CONTENT_DIR, f)).isDirectory()
    );

    for (const classFolder of classFolders) {
        const classNum = getClassNumber(classFolder);
        if (!classNum) continue;

        const classPath = path.join(CONTENT_DIR, classFolder);
        const subjects = fs.readdirSync(classPath).filter(f =>
            fs.statSync(path.join(classPath, f)).isDirectory()
        );

        for (const subject of subjects) {
            const subjectPath = path.join(classPath, subject);
            const chapters = fs.readdirSync(subjectPath).filter(f =>
                f.endsWith('.json') && f.startsWith('ch')
            );

            for (const chapterFile of chapters) {
                const chapterMatch = chapterFile.match(/ch(\d+)\.json/);
                if (!chapterMatch) continue;

                const chapterNum = parseInt(chapterMatch[1]);
                const filePath = path.join(subjectPath, chapterFile);

                files.push({
                    classNum,
                    subject,
                    chapterNum,
                    filePath
                });
            }
        }
    }

    return files;
}

// Generate placeholder content for all chapters
function generatePlaceholderContent(classNum, subject, chapterNum) {
    return {
        chapter_title: `Chapter ${chapterNum}`,
        sections: [
            {
                type: 'text',
                title: 'Introduction',
                content: `This is placeholder content for Class ${classNum}, ${subject}, Chapter ${chapterNum}.\n\nLorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.\n\nDuis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.`
            },
            {
                type: 'youtube',
                title: 'Video Lesson',
                content: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ'
            },
            {
                type: 'image',
                title: 'Diagram',
                content: 'https://via.placeholder.com/800x400/1976D2/FFFFFF?text=Class+' + classNum + '+Chapter+' + chapterNum
            },
            {
                type: 'text',
                title: 'Summary',
                content: 'This section will contain the summary and key points of the lesson. Edit this content through the Admin Dashboard to add your actual lesson material.'
            }
        ]
    };
}

// Send content to API
function uploadContent(classNum, subject, chapterNum, chapterTitle, contentJson) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify({
            class: classNum,
            subject: subject,
            chapter_number: chapterNum,
            chapter_title: chapterTitle,
            content_json: contentJson
        });

        const url = new URL(`${API_URL}/content`);
        const options = {
            hostname: url.hostname,
            port: url.port || 443,
            path: url.pathname,
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(data)
            }
        };

        const req = https.request(options, (res) => {
            let body = '';
            res.on('data', chunk => body += chunk);
            res.on('end', () => {
                if (res.statusCode === 200) {
                    resolve({ success: true });
                } else {
                    reject(new Error(`HTTP ${res.statusCode}: ${body}`));
                }
            });
        });

        req.on('error', reject);
        req.write(data);
        req.end();
    });
}

// Main migration function
async function migrate() {
    console.log('🚀 Starting content migration with placeholder content...\n');

    const files = getAllContentFiles();
    console.log(`Found ${files.length} chapter files to migrate\n`);

    let success = 0;
    let failed = 0;

    for (const file of files) {
        try {
            // Generate placeholder content for this chapter
            const placeholderContent = generatePlaceholderContent(
                file.classNum,
                file.subject,
                file.chapterNum
            );

            // Upload to database
            await uploadContent(
                file.classNum,
                file.subject,
                file.chapterNum,
                placeholderContent.chapter_title,
                placeholderContent
            );

            console.log(`✅ Created: Class ${file.classNum} / ${file.subject} / Ch ${file.chapterNum}`);
            success++;

        } catch (error) {
            console.error(`❌ Failed: Class ${file.classNum} / ${file.subject} / Ch ${file.chapterNum} - ${error.message}`);
            failed++;
        }
    }

    console.log('\n📊 Migration Summary:');
    console.log(`   ✅ Success: ${success}`);
    console.log(`   ❌ Failed: ${failed}`);
    console.log(`   📁 Total files: ${files.length}`);
}

// Run migration
migrate().catch(console.error);
