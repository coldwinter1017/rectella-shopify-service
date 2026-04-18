using './main.bicep'

// Network — match the existing VNet
param vnetName = 'Rectella-Network'
param dbSubnetPrefix = '10.0.2.0/24'
param appsSubnetPrefix = '10.0.4.0/23'

// Container image
param containerImage = 'ghcr.io/trismegistus0/rectella-shopify-service:latest'

// PostgreSQL admin
param pgAdminUsername = 'rectella'
param pgAdminPassword = readEnvironmentVariable('PG_ADMIN_PASSWORD')

// Application env vars — fill these in before deploying
param shopifyWebhookSecret = readEnvironmentVariable('SHOPIFY_WEBHOOK_SECRET', '')
param shopifyStoreUrl = 'rectella.myshopify.com'
param shopifyAccessToken = readEnvironmentVariable('SHOPIFY_ACCESS_TOKEN', '')
param shopifyLocationId = ''

param sysproEnetUrl = 'http://192.168.3.150:31002/SYSPROWCFService/Rest'
param sysproOperator = 'ctrlaltinsight'
param sysproPassword = readEnvironmentVariable('SYSPRO_PASSWORD', '')
param sysproCompanyId = readEnvironmentVariable('SYSPRO_COMPANY_ID', '')
param sysproWarehouse = ''
param sysproSkus = ''

param adminToken = readEnvironmentVariable('ADMIN_TOKEN', '')
param logLevel = 'info'
