DELETE FROM ykt_aisaas_model_registry WHERE id = 1;
DELETE FROM ykt_aisaas_balance WHERE id = 1;
DELETE FROM ykt_aisaas_quota WHERE id IN (1,2);
DELETE FROM ykt_aisaas_internal_tenant_config WHERE id = 1;
DELETE FROM ykt_aisaas_tenant WHERE id IN (1, 1001);
