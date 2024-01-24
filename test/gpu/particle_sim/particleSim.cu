#include <stdio.h>
#include <cuda_runtime.h>

typedef struct {
    float x, y, z; // Position
    float vx, vy, vz; // Velocity
} Particle;

__global__ void updateParticles(Particle *particles, int numParticles, float deltaTime) {
    int idx = blockIdx.x * blockDim.x + threadIdx.x;
    if (idx < numParticles) {
        // Simple physics update
        particles[idx].vy -= 9.81f * deltaTime; // Gravity effect

        // Update position
        particles[idx].x += particles[idx].vx * deltaTime;
        particles[idx].y += particles[idx].vy * deltaTime;
        particles[idx].z += particles[idx].vz * deltaTime;
    }
}

void saveParticlePositions(Particle *particles, int numParticles, FILE *file) {
    for (int i = 0; i < numParticles; i++) {
        fprintf(file, "%f %f %f\n", particles[i].x, particles[i].y, particles[i].z);
    }
    fprintf(file, "\n"); // Separate different frames by a newline
}

int main() {
    int numParticles = 1000;
    float deltaTime = 0.01f; // Time step for simulation
    int numIterations = 100; // Number of simulation steps

    Particle *particles_host = (Particle*)malloc(numParticles * sizeof(Particle));
    Particle *particles_device;

    // Initialize particles
    // ...

    cudaMalloc((void**)&particles_device, numParticles * sizeof(Particle));
    cudaMemcpy(particles_device, particles_host, numParticles * sizeof(Particle), cudaMemcpyHostToDevice);

    FILE *file = fopen("particle_positions.txt", "w");
    if (file == NULL) {
        fprintf(stderr, "Failed to open the file for writing.\n");
        exit(1);
    }

    int threadsPerBlock = 256;
    int blocksPerGrid = (numParticles + threadsPerBlock - 1) / threadsPerBlock;

    for (int iter = 0; iter < numIterations; iter++) {
        updateParticles<<<blocksPerGrid, threadsPerBlock>>>(particles_device, numParticles, deltaTime);
        cudaMemcpy(particles_host, particles_device, numParticles * sizeof(Particle), cudaMemcpyDeviceToHost);
        saveParticlePositions(particles_host, numParticles, file);
    }

    fclose(file);
    cudaFree(particles_device);
    free(particles_host);

    return 0;
}
