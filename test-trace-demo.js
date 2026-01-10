/**
 * Demo script showing trace output
 * Run with: node test-trace-demo.js
 */

const { createTrace } = require('./src/trace');

async function demoTrace() {
    console.log('=== Trace Disabled (backward compatible) ===');
    const noTrace = createTrace({ trace: false });
    
    await noTrace.stage('test-stage', async () => {
        console.log('Executing work...');
    });
    
    const result1 = noTrace.finalize();
    console.log('Result:', result1); // Should be null
    
    console.log('\n=== Trace Enabled ===');
    const withTrace = createTrace({ 
        trace: true, 
        includeSnapshot: false 
    });
    
    // Simulate pipeline stages
    await withTrace.stage('scenario-parse', async () => {
        // Simulate parsing
        await new Promise(r => setTimeout(r, 10));
    });
    withTrace.setSummary('scenario-parse', {
        serviceIdResolved: 'default:frontend',
        maxDepth: 2
    });
    
    await withTrace.stage('staleness-check', async () => {
        await new Promise(r => setTimeout(r, 5));
    });
    withTrace.setSummary('staleness-check', {
        stale: false,
        lastUpdatedSecondsAgo: 30,
        windowMinutes: 5
    });
    
    await withTrace.stage('fetch-neighborhood', async () => {
        await new Promise(r => setTimeout(r, 50));
    });
    withTrace.setSummary('fetch-neighborhood', {
        depthUsed: 2,
        nodesReturned: 12,
        edgesReturned: 18
    });
    
    await withTrace.stage('build-snapshot', async () => {
        await new Promise(r => setTimeout(r, 15));
    });
    withTrace.setSummary('build-snapshot', {
        serviceCount: 12,
        edgeCount: 18
    });
    
    await withTrace.stage('path-analysis', async () => {
        await new Promise(r => setTimeout(r, 25));
    });
    withTrace.setSummary('path-analysis', {
        pathsFound: 15,
        pathsReturned: 10
    });
    
    await withTrace.stage('compute-impact', async () => {
        await new Promise(r => setTimeout(r, 20));
    });
    withTrace.setSummary('compute-impact', {
        affectedCallersCount: 3,
        unreachableCount: 0,
        totalLostTrafficRps: 150.5
    });
    
    await withTrace.stage('recommendations', async () => {
        await new Promise(r => setTimeout(r, 8));
    });
    withTrace.setSummary('recommendations', {
        recommendationCount: 2
    });
    
    const result2 = withTrace.finalize();
    console.log(JSON.stringify(result2, null, 2));
}

demoTrace().catch(console.error);
