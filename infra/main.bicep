// Rectella Shopify Service — Azure infrastructure
//
// Deploys the missing pieces on top of the existing VPN stack:
//   - db-subnet (delegated to PostgreSQL Flexible Server)
//   - apps-subnet (delegated to Container Apps)
//   - Private DNS zone for PostgreSQL + VNet link
//   - PostgreSQL Flexible Server (Burstable B1ms, PG 16)
//   - Container Apps Environment (VNet-integrated)
//   - Container App pulling from ghcr.io
//
// Pre-existing (not managed here): Rectella-Network, RectellaVPN,
// Office-Meraki, Azure-to-Office connection.

targetScope = 'resourceGroup'

@description('Azure region — must match the existing VNet')
param location string = resourceGroup().location

@description('Name of the existing VNet')
param vnetName string = 'Rectella-Network'

@description('CIDR for the new PostgreSQL delegated subnet')
param dbSubnetPrefix string = '10.0.2.0/24'

@description('CIDR for the new Container Apps delegated subnet (/23 minimum)')
param appsSubnetPrefix string = '10.0.4.0/23'

@description('PostgreSQL admin username')
param pgAdminUsername string = 'rectella'

@description('PostgreSQL admin password')
@secure()
param pgAdminPassword string

@description('Container image reference')
param containerImage string = 'ghcr.io/trismegistus0/rectella-shopify-service:latest'

// ---- Application env vars (passed as Container App secrets) ----

@secure()
param shopifyWebhookSecret string

param shopifyStoreUrl string = 'rectella.myshopify.com'

@secure()
param shopifyAccessToken string

param shopifyLocationId string = ''

param sysproEnetUrl string = 'http://192.168.3.150:31002/SYSPROWCFService/Rest'

param sysproOperator string

@secure()
param sysproPassword string

param sysproCompanyId string

param sysproWarehouse string

param sysproSkus string

@secure()
param adminToken string

param logLevel string = 'info'

// ---- Existing resources ----

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' existing = {
  name: vnetName
}

// ---- New subnets (attached to existing VNet) ----

resource dbSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' = {
  parent: vnet
  name: 'db-subnet'
  properties: {
    addressPrefix: dbSubnetPrefix
    delegations: [
      {
        name: 'postgres-delegation'
        properties: {
          serviceName: 'Microsoft.DBforPostgreSQL/flexibleServers'
        }
      }
    ]
  }
}

resource appsSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' = {
  parent: vnet
  name: 'apps-subnet'
  properties: {
    addressPrefix: appsSubnetPrefix
    delegations: [
      {
        name: 'container-apps-delegation'
        properties: {
          serviceName: 'Microsoft.App/environments'
        }
      }
    ]
  }
  dependsOn: [
    dbSubnet
  ]
}

// ---- Private DNS zone for PostgreSQL ----

resource pgDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' = {
  name: 'rectella.private.postgres.database.azure.com'
  location: 'global'
}

resource pgDnsLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  parent: pgDnsZone
  name: 'rectella-vnet-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

// ---- PostgreSQL Flexible Server ----

resource postgres 'Microsoft.DBforPostgreSQL/flexibleServers@2024-08-01' = {
  name: 'rectella-db'
  location: location
  sku: {
    name: 'Standard_B1ms'
    tier: 'Burstable'
  }
  properties: {
    version: '16'
    administratorLogin: pgAdminUsername
    administratorLoginPassword: pgAdminPassword
    storage: {
      storageSizeGB: 32
    }
    backup: {
      backupRetentionDays: 7
      geoRedundantBackup: 'Disabled'
    }
    highAvailability: {
      mode: 'Disabled'
    }
    network: {
      delegatedSubnetResourceId: dbSubnet.id
      privateDnsZoneArmResourceId: pgDnsZone.id
    }
  }
  dependsOn: [
    pgDnsLink
  ]
}

resource rectellaDb 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: postgres
  name: 'rectella'
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
}

// ---- Container Apps Environment ----

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: 'rectella-logs'
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: 30
  }
}

resource containerEnv 'Microsoft.App/managedEnvironments@2025-01-01' = {
  name: 'rectella-env'
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalytics.properties.customerId
        sharedKey: logAnalytics.listKeys().primarySharedKey
      }
    }
    vnetConfiguration: {
      infrastructureSubnetId: appsSubnet.id
      internal: false
    }
    workloadProfiles: [
      {
        name: 'Consumption'
        workloadProfileType: 'Consumption'
      }
    ]
  }
}

// ---- Container App ----

resource containerApp 'Microsoft.App/containerApps@2025-01-01' = {
  name: 'rectella-shopify-service'
  location: location
  properties: {
    managedEnvironmentId: containerEnv.id
    workloadProfileName: 'Consumption'
    configuration: {
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        allowInsecure: false
        traffic: [
          {
            weight: 100
            latestRevision: true
          }
        ]
      }
      secrets: [
        { name: 'database-url', value: 'postgres://${pgAdminUsername}:${pgAdminPassword}@${postgres.properties.fullyQualifiedDomainName}:5432/rectella?sslmode=require' }
        { name: 'shopify-webhook-secret', value: shopifyWebhookSecret }
        { name: 'shopify-access-token', value: shopifyAccessToken }
        { name: 'syspro-password', value: sysproPassword }
        { name: 'admin-token', value: adminToken }
      ]
    }
    template: {
      containers: [
        {
          name: 'rectella-shopify-service'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: [
            { name: 'PORT', value: '8080' }
            { name: 'LOG_LEVEL', value: logLevel }
            { name: 'DATABASE_URL', secretRef: 'database-url' }
            { name: 'SHOPIFY_WEBHOOK_SECRET', secretRef: 'shopify-webhook-secret' }
            { name: 'SHOPIFY_STORE_URL', value: shopifyStoreUrl }
            { name: 'SHOPIFY_ACCESS_TOKEN', secretRef: 'shopify-access-token' }
            { name: 'SHOPIFY_LOCATION_ID', value: shopifyLocationId }
            { name: 'SYSPRO_ENET_URL', value: sysproEnetUrl }
            { name: 'SYSPRO_OPERATOR', value: sysproOperator }
            { name: 'SYSPRO_PASSWORD', secretRef: 'syspro-password' }
            { name: 'SYSPRO_COMPANY_ID', value: sysproCompanyId }
            { name: 'SYSPRO_WAREHOUSE', value: sysproWarehouse }
            { name: 'SYSPRO_SKUS', value: sysproSkus }
            { name: 'ADMIN_TOKEN', secretRef: 'admin-token' }
            { name: 'BATCH_INTERVAL', value: '5m' }
            { name: 'STOCK_SYNC_INTERVAL', value: '15m' }
            { name: 'FULFILMENT_SYNC_INTERVAL', value: '30m' }
          ]
          probes: [
            {
              type: 'Liveness'
              httpGet: {
                path: '/health'
                port: 8080
              }
              initialDelaySeconds: 30
              periodSeconds: 30
            }
            {
              type: 'Readiness'
              httpGet: {
                path: '/ready'
                port: 8080
              }
              initialDelaySeconds: 5
              periodSeconds: 10
            }
          ]
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
}

// ---- Outputs ----

output containerAppFqdn string = containerApp.properties.configuration.ingress.fqdn
output postgresFqdn string = postgres.properties.fullyQualifiedDomainName
output webhookUrl string = 'https://${containerApp.properties.configuration.ingress.fqdn}/webhooks/orders/create'
