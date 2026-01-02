$ProjectId = "eva-mind-sdk"
$Region = "southamerica-east1"
$ServiceName = "eva-mind-sdk"
$ImageName = "gcr.io/$ProjectId/$ServiceName"

Write-Host "🚀 Iniciando Deploy do EVA-Mind..." -ForegroundColor Green

# 1. Build Local (Verificação)
Write-Host "🛠️ Compilando localmente para verificar erros..."
go build -o eva-mind.exe
if ($LASTEXITCODE -ne 0) {
    Write-Error "❌ Falha na compilação local!"
    exit 1
}

# 2. Build Container Google Cloud
Write-Host "☁️ Construindo imagem no Cloud Build..."
gcloud builds submit --tag $ImageName .
if ($LASTEXITCODE -ne 0) {
    Write-Error "❌ Falha no Cloud Build!"
    exit 1
}

# 3. Deploy Cloud Run
Write-Host "🚀 Realizando Deploy no Cloud Run..."
gcloud run deploy $ServiceName `
    --image $ImageName `
    --platform managed `
    --region $Region `
    --allow-unauthenticated `
    --port 8080 `
    --set-env-vars "ENVIRONMENT=production"

if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ Deploy concluído com SUCESSO!" -ForegroundColor Green
    gcloud run services describe $ServiceName --region $Region --format "value(status.url)"
} else {
    Write-Error "❌ Falha no Deploy!"
}
