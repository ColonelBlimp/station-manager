package facade

// CatStatus returns nil and is defined only to export the method signature.
// NOTE: the return type is not types.CatStatus, because this type would not be
// understood by the frontend.
func (s *Service) CatStatus() map[string]string {
	return nil
}
