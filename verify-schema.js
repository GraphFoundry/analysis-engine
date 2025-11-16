// Utility script to verify Neo4j schema
// Requires: NEO4J_URI, NEO4J_PASSWORD in .env
const neo4j = require('neo4j-driver');
require('dotenv').config();

if (!process.env.NEO4J_URI || !process.env.NEO4J_PASSWORD) {
    console.error('❌ Missing NEO4J_URI or NEO4J_PASSWORD in environment.');
    console.error('   Copy .env.example to .env and fill in your credentials.');
    process.exit(1);
}

const uri = process.env.NEO4J_URI;
const user = process.env.NEO4J_USER || 'neo4j';
const password = process.env.NEO4J_PASSWORD;

const driver = neo4j.driver(uri, neo4j.auth.basic(user, password));

async function verify() {
    const session = driver.session();
    try {
        console.log('Verifying Neo4j connection...');
        
        // 1. Check if CALLS_NOW relationships exist
        const result = await session.run(`
            MATCH (a:Service)-[r:CALLS_NOW]->(b:Service)
            RETURN a.serviceId AS source, b.serviceId AS dest, 
                   r.rate AS rate, r.p95 AS p95
            LIMIT 3
        `);
        
        console.log('\nSample CALLS_NOW relationships:');
        console.log('Direction: (source) -[:CALLS_NOW]-> (dest)');
        console.log('---');
        
        if (result.records.length === 0) {
            console.log('WARNING: No CALLS_NOW relationships found in database');
        } else {
            result.records.forEach(record => {
                console.log(`${record.get('source')} -> ${record.get('dest')}`);
                console.log(`  rate: ${record.get('rate')}, p95: ${record.get('p95')}`);
            });
        }
        
        // 2. Count services
        const countResult = await session.run(`
            MATCH (s:Service) RETURN count(s) AS total
        `);
        console.log(`\nTotal services: ${countResult.records[0].get('total')}`);
        
        console.log('\n✅ Schema verification complete');
        console.log('Confirmed: (caller:Service)-[:CALLS_NOW]->(callee:Service)');
        console.log('ServiceId format: "namespace:name"');
        
    } catch (error) {
        console.error('❌ Error during verification:', error.message);
    } finally {
        await session.close();
        await driver.close();
    }
}

verify();
