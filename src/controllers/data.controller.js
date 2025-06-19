// src/controllers/data.controller.js
import { query } from '../services/db.js';

const VALID_TYPES = {
    paper: { table: 'paper', validSorts: ['created_at', 'title', 'journal', 'year', 'cid'] },
    genome: { table: 'genome', validSorts: ['created_at', 'organism', 'assembly_version', 'cid'] },
    spectrum: { table: 'spectrum', validSorts: ['created_at', 'compound', 'technique_nmr_ir_ms', 'cid'] },
    file_cids: { table: 'file_cids', validSorts: ['uploaded_at', 'filename', 'cid', 'id'] },
};

function buildWhereClause(type, queryParams) {
    // ... logic remains identical to CJS version ...
    const whereClauses = [];
    const args = [];
    let argIndex = 1;

    if (queryParams.search) {
        switch (type) {
            case 'paper':
                whereClauses.push(`(title ILIKE $${argIndex} OR journal ILIKE $${argIndex})`);
                args.push(`%${queryParams.search}%`);
                argIndex++;
                break;
            case 'genome':
                whereClauses.push(`(organism ILIKE $${argIndex} OR notes ILIKE $${argIndex})`);
                args.push(`%${queryParams.search}%`);
                argIndex++;
                break;
            case 'spectrum':
                whereClauses.push(`(compound ILIKE $${argIndex})`);
                args.push(`%${queryParams.search}%`);
                argIndex++;
                break;
            case 'file_cids':
                whereClauses.push(`(filename ILIKE $${argIndex})`);
                args.push(`%${queryParams.search}%`);
                argIndex++;
                break;
        }
    }

    if (type === 'paper') {
        if (queryParams.year) { whereClauses.push(`year = $${argIndex++}`); args.push(Number(queryParams.year)); }
        if (queryParams.journal) { whereClauses.push(`journal ILIKE $${argIndex++}`); args.push(`%${queryParams.journal}%`); }
        if (queryParams.keyword) { whereClauses.push(`$${argIndex++} = ANY(keywords)`); args.push(queryParams.keyword); }
    }
    if (type === 'genome') {
        if (queryParams.organism) { whereClauses.push(`organism ILIKE $${argIndex++}`); args.push(`%${queryParams.organism}%`); }
        if (queryParams.assembly) { whereClauses.push(`assembly_version ILIKE $${argIndex++}`); args.push(`%${queryParams.assembly}%`); }
    }
     if (type === 'spectrum') {
        if (queryParams.compound) { whereClauses.push(`compound ILIKE $${argIndex++}`); args.push(`%${queryParams.compound}%`); }
        if (queryParams.technique) { whereClauses.push(`technique_nmr_ir_ms ILIKE $${argIndex++}`); args.push(`%${queryParams.technique}%`); }
    }

    const whereString = whereClauses.length > 0 ? `WHERE ${whereClauses.join(' AND ')}` : '';
    return { whereString, args, argIndex };
}

export async function queryDataHandler(req, res, next) {
    try {
        const { type } = req.params;
        if (!VALID_TYPES[type]) return res.status(400).json({ error: 'Invalid data type' });
        
        const { table, validSorts } = VALID_TYPES[type];
        
        const limit = parseInt(req.query.limit, 10) || 20;
        const offset = parseInt(req.query.offset, 10) || 0;
        const sortBy = validSorts.includes(req.query.sort) ? req.query.sort : validSorts[0];
        const sortOrder = ['ASC', 'DESC'].includes(req.query.order?.toUpperCase()) ? req.query.order.toUpperCase() : 'DESC';

        const { whereString, args, argIndex } = buildWhereClause(type, req.query);
        
        const countQuery = `SELECT COUNT(*) FROM ${table} ${whereString}`;
        const countResult = await query(countQuery, args);
        const totalCount = parseInt(countResult.rows[0].count, 10);
        
        const dataQuery = `SELECT * FROM ${table} ${whereString} ORDER BY ${sortBy} ${sortOrder} LIMIT $${argIndex} OFFSET $${argIndex + 1}`;
        const finalArgs = [...args, limit, offset];
        const dataResult = await query(dataQuery, finalArgs);

        res.status(200).json({
            data: dataResult.rows,
            pagination: { total: totalCount, limit, offset, count: dataResult.rows.length },
            sort: { by: sortBy, order: sortOrder },
        });
    } catch (error) {
        console.error('[API ERROR] in queryDataHandler:', error);
        next(error);
    }
}

export async function getDataByIDHandler(req, res, next) {
    try {
        const { type, cid } = req.params;
        if (!VALID_TYPES[type]) return res.status(400).json({ error: 'Invalid data type' });
        if (!cid) return res.status(400).json({ error: 'CID is required' });

        const { table } = VALID_TYPES[type];
        const result = await query(`SELECT * FROM ${table} WHERE cid = $1`, [cid]);

        if (result.rows.length === 0) return res.status(404).json({ error: 'Record not found' });
        
        res.status(200).json({ data: result.rows[0] });
    } catch (error) {
        console.error('[API ERROR] in getDataByIDHandler:', error);
        next(error);
    }
}

export async function listCIDsHandler(req, res, next) {
    try {
        const { filename } = req.query;
        const sql = filename 
            ? 'SELECT filename, cid, uploaded_at FROM file_cids WHERE filename = $1 ORDER BY uploaded_at DESC'
            : 'SELECT filename, cid, uploaded_at FROM file_cids ORDER BY uploaded_at DESC';
        const params = filename ? [filename] : [];
        
        const result = await query(sql, params);
        res.status(200).json(result.rows);
    } catch (error) {
        console.error('[API ERROR] in listCIDsHandler:', error);
        next(error);
    }
}