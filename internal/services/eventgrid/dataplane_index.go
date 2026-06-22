package eventgrid

const dataPlaneTopicsBucket = "eventgrid.dataplane.topics"

// El data-plane (dataplane.go) recibe requests con la URL
// "/{topicName}.eventgrid/api/events", que solo trae el nombre del topic —
// no la subscriptionId/resourceGroup que sí tiene la clave de topicsBucket
// ni las de eventSubscriptionsBucket. Este índice pequeño y plano (clave =
// nombre del topic, valor = "subID/rg") permite, al publicar, recuperar el
// par subID/rg del topic y de ahí listar sus event subscriptions con el
// prefijo correcto — mismo patrón que
// servicebus/dataplane_index.go usa para namespaces/queues/topics.
//
// A diferencia de Service Bus (donde varios tipos de sub-recurso comparten
// el mismo bucket plano y necesitan prefijos distintos para no
// colisionar), aquí solo hay un tipo de entidad de nivel superior (el
// topic), así que el nombre del topic alcanza como clave sin necesidad de
// un prefijo adicional.
type dataPlaneTopicRef struct {
	SubscriptionID string `json:"subscriptionId"`
	ResourceGroup  string `json:"resourceGroup"`
}

func (s *Service) markDataPlaneTopic(name, subID, rg string) error {
	return s.db.Put(dataPlaneTopicsBucket, name, dataPlaneTopicRef{SubscriptionID: subID, ResourceGroup: rg})
}

func (s *Service) unmarkDataPlaneTopic(name string) error {
	return s.db.Delete(dataPlaneTopicsBucket, name)
}

func (s *Service) lookupDataPlaneTopic(name string) (dataPlaneTopicRef, bool, error) {
	var ref dataPlaneTopicRef
	found, err := s.db.Get(dataPlaneTopicsBucket, name, &ref)
	return ref, found, err
}
