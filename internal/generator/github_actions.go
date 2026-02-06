package generator

import (
	"fmt"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// GenerateGitHubActions generates a GitHub Actions workflow
func GenerateGitHubActions(analysis *types.AppAnalysis, cfg *config.Config) (string, error) {
	registry := cfg.CI.Registry
	if registry == "" {
		registry = "ghcr.io/${{ github.repository_owner }}"
	}

	imageName := fmt.Sprintf("%s/%s", registry, analysis.Name)

	workflow := fmt.Sprintf(`name: Build and Deploy

on:
  push:
    branches:
      - main
      - master
  pull_request:
    branches:
      - main
      - master

env:
  REGISTRY: %s
  IMAGE_NAME: %s

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Container Registry
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.IMAGE_NAME }}
          tags: |
            type=ref,event=branch
            type=ref,event=pr
            type=sha,prefix=
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

  deploy:
    needs: build
    runs-on: ubuntu-latest
    if: github.event_name != 'pull_request'
    
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Update image tag in manifests
        run: |
          SHORT_SHA=$(echo ${{ github.sha }} | cut -c1-7)
          sed -i "s|image: .*%s.*|image: ${{ env.IMAGE_NAME }}:${SHORT_SHA}|g" k8s/deployment.yaml

      - name: Commit and push changes
        run: |
          git config --local user.email "github-actions[bot]@users.noreply.github.com"
          git config --local user.name "github-actions[bot]"
          git add k8s/
          git diff --staged --quiet || git commit -m "chore: update image to ${{ github.sha }}"
          git push
`, registry, imageName, analysis.Name)

	return workflow, nil
}
