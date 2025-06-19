// src/controllers/upload.controller.js
import { query } from '../services/db.js';
import { uploadData } from '../services/synapse.js';

export async function uploadAndAddRootHandler(req, res, next) {
    try {
        const { proofSetID } = req.body;
        if (!req.file) {
            return res.status(400).json({ error: 'file is required' });
        }

        console.log(`[API] Received file ${req.file.originalname} (${req.file.size} bytes) for proof set ${proofSetID || 'any'}`);

        const uploadResult = await uploadData(req.file.buffer, { proofSetId: proofSetID ? Number(proofSetID) : undefined });

        // Save mapping to DB
        await query(
            'INSERT INTO file_cids (filename, cid) VALUES ($1, $2) ON CONFLICT (cid) DO NOTHING',
            [req.file.originalname, uploadResult.commp]
        );
        console.log(`[DB] Saved generic mapping: ${req.file.originalname} -> ${uploadResult.commp}`);

        res.status(200).json({
            proofSetID: uploadResult.proofSetId,
            rootCID: uploadResult.commp, // Keep name for compatibility
            message: "File uploaded and root added successfully",
        });

    } catch (error) {
        console.error('[API ERROR] in uploadAndAddRootHandler:', error);
        next(error);
    }
}


export async function uploadAndAddPaperHandler(req, res, next) {
    try {
        const { title, journal, year, keywords, authors } = req.body;
        if (!req.file || !title) {
            return res.status(400).json({ error: 'file and title are required' });
        }
        console.log(`[API] Uploading paper: "${title}"`);

        const uploadResult = await uploadData(req.file.buffer);
        const commP = uploadResult.commp;

        const keywordsArray = keywords ? keywords.split(',').map(k => k.trim()) : [];
        const authorsArray = authors ? authors.split(',').map(a => a.trim()) : [];

        // Save to paper table
        await query(
            `INSERT INTO paper (cid, title, journal, year, keywords, authors) 
             VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (cid) DO NOTHING`,
            [commP, title, journal || null, year ? Number(year) : null, keywordsArray, authorsArray]
        );
        console.log(`[DB] Saved paper metadata for CommP: ${commP}`);

        // Save to file_cids for compatibility
        await query(
            'INSERT INTO file_cids (filename, cid) VALUES ($1, $2) ON CONFLICT (cid) DO NOTHING',
            [req.file.originalname, commP]
        );

        res.status(200).json({
            proofSetID: uploadResult.proofSetId,
            rootCID: commP,
            title,
            journal,
            year: year ? Number(year) : null,
            keywords: keywordsArray,
            authors: authorsArray,
        });

    } catch (error) {
        console.error('[API ERROR] in uploadAndAddPaperHandler:', error);
        next(error);
    }
}


export async function uploadAndAddGenomeHandler(req, res, next) {
    try {
        const { organism, assemblyVersion, notes } = req.body;
        if (!req.file || !organism) {
            return res.status(400).json({ error: 'file and organism are required' });
        }
        console.log(`[API] Uploading genome for: "${organism}"`);

        const uploadResult = await uploadData(req.file.buffer);
        const commP = uploadResult.commp;

        await query(
            'INSERT INTO genome (cid, organism, assembly_version, notes) VALUES ($1, $2, $3, $4) ON CONFLICT (cid) DO NOTHING',
            [commP, organism, assemblyVersion || null, notes || null]
        );
        console.log(`[DB] Saved genome metadata for CommP: ${commP}`);

        await query(
            'INSERT INTO file_cids (filename, cid) VALUES ($1, $2) ON CONFLICT (cid) DO NOTHING',
            [req.file.originalname, commP]
        );

        res.status(200).json({
            proofSetID: uploadResult.proofSetId,
            rootCID: commP,
            organism,
            assemblyVersion,
            notes,
        });

    } catch (error) {
        console.error('[API ERROR] in uploadAndAddGenomeHandler:', error);
        next(error);
    }
}

export async function uploadAndAddSpectrumHandler(req, res, next) {
    try {
        const { compound, technique, metadata } = req.body;
        if (!req.file || !compound) {
            return res.status(400).json({ error: 'file and compound are required' });
        }
        console.log(`[API] Uploading spectrum for: "${compound}"`);

        let metadataJsonb = null;
        if (metadata) {
            try {
                metadataJsonb = JSON.parse(metadata);
            } catch (e) {
                return res.status(400).json({ error: 'Invalid JSON metadata' });
            }
        }

        const uploadResult = await uploadData(req.file.buffer);
        const commP = uploadResult.commp;

        await query(
            'INSERT INTO spectrum (cid, compound, technique_nmr_ir_ms, metadata_json) VALUES ($1, $2, $3, $4) ON CONFLICT (cid) DO NOTHING',
            [commP, compound, technique || null, metadataJsonb]
        );
        console.log(`[DB] Saved spectrum metadata for CommP: ${commP}`);

        await query(
            'INSERT INTO file_cids (filename, cid) VALUES ($1, $2) ON CONFLICT (cid) DO NOTHING',
            [req.file.originalname, commP]
        );

        res.status(200).json({
            proofSetID: uploadResult.proofSetId,
            rootCID: commP,
            compound,
            technique,
            metadata: metadataJsonb,
        });
    } catch (error) {
        console.error('[API ERROR] in uploadAndAddSpectrumHandler:', error);
        next(error);
    }
}