// Simple test to directly call the simulation functions
require('dotenv').config();
const { fetchUpstreamNeighborhood } = require('./src/graph');
const { simulateFailure } = require('./src/failureSimulation');

async function test() {
    try {
        console.log('Testing fetchUpstreamNeighborhood for default:frontend with depth 2...');
        const snapshot = await fetchUpstreamNeighborhood('default:frontend', 2);
        console.log('✓ Graph snapshot retrieved successfully');
        console.log(`  Nodes: ${snapshot.nodes.size}`);
        console.log(`  Edges: ${snapshot.edges.length}`);
        
        console.log('\nTesting simulateFailure...');
        const result = await simulateFailure({ serviceId: 'default:frontend', maxDepth: 2 });
        console.log('✓ Failure simulation completed');
        console.log(`  Affected services: ${result.affectedCallers.length}`);
        console.log(`  Top paths: ${result.criticalPathsBroken.length}`);
        
        console.log('\n=== Success ===');
        process.exit(0);
    } catch (error) {
        console.error('✗ Error:', error.message);
        console.error(error.stack);
        process.exit(1);
    }
}

test();
