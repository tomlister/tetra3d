package tetra3d

import (
	"bytes"
	"math"
	"os"

	"github.com/kvartborg/vector"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// LoadGLTFFile loads a .gltf or .glb file from the path given. It will return a SceneCollection and an error if the process fails. Animations (including armature-based animations)
// will be parsed properly, but skinned meshes should be at origin (0, 0, 0) to be properly loaded.
func LoadGLTFFile(path string) (*Library, error) {

	fileData, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	return LoadGLTFData(fileData)

}

// LoadGLTFData loads a .gltf or .glb file loaded in as a sequence of bytes, returning a SceneCollection and error if the process fails. Animations (including armature-based animations)
// will be parsed properly, but skinned meshes should be at origin (0, 0, 0) to be properly loaded.
func LoadGLTFData(data []byte) (*Library, error) {

	decoder := gltf.NewDecoder(bytes.NewReader(data))

	doc := gltf.NewDocument()

	err := decoder.Decode(doc)

	if err != nil {
		return nil, err
	}

	collection := NewLibrary()

	type VertexData struct {
		Pos        vector.Vector
		UV         vector.Vector
		Normal     vector.Vector
		Color      *Color
		WeightData []float32
		Bones      []uint16
	}

	verticesToVertexData := map[*Vertex]VertexData{}

	for _, mesh := range doc.Meshes {

		newMesh := NewMesh(mesh.Name)
		collection.Meshes[mesh.Name] = newMesh

		for _, v := range mesh.Primitives {

			posBuffer := [][3]float32{}
			vertPos, err := modeler.ReadPosition(doc, doc.Accessors[v.Attributes[gltf.POSITION]], posBuffer)

			if err != nil {
				return nil, err
			}

			vertexData := []VertexData{}

			for _, v := range vertPos {

				vertexData = append(vertexData, VertexData{
					Pos:   vector.Vector{float64(v[0]), float64(v[1]), float64(v[2])},
					Color: NewColor(1, 1, 1, 1),
				})

			}

			if texCoordAccessor, texCoordExists := v.Attributes[gltf.TEXCOORD_0]; texCoordExists {

				uvBuffer := [][2]float32{}

				texCoords, err := modeler.ReadTextureCoord(doc, doc.Accessors[texCoordAccessor], uvBuffer)

				if err != nil {
					return nil, err
				}

				for i, v := range texCoords {
					vertexData[i].UV = vector.Vector{float64(v[0]), float64(v[1])}
				}

			}

			if normalAccessor, normalExists := v.Attributes[gltf.NORMAL]; normalExists {

				normalBuffer := [][3]float32{}

				texCoords, err := modeler.ReadNormal(doc, doc.Accessors[normalAccessor], normalBuffer)

				if err != nil {
					return nil, err
				}

				for i, v := range texCoords {
					vertexData[i].Normal = vector.Vector{float64(v[0]), float64(v[1]), float64(v[2])}
				}

			}

			if vertexColorAccessor, vcExists := v.Attributes[gltf.COLOR_0]; vcExists {

				vcBuffer := [][4]uint16{}

				colors, err := modeler.ReadColor64(doc, doc.Accessors[vertexColorAccessor], vcBuffer)

				if err != nil {
					return nil, err
				}

				for i, v := range colors {

					vertexData[i].Color.R = float32(v[0]) / math.MaxUint16
					vertexData[i].Color.G = float32(v[1]) / math.MaxUint16
					vertexData[i].Color.B = float32(v[2]) / math.MaxUint16
					vertexData[i].Color.A = float32(v[3]) / math.MaxUint16

				}

			}

			if weightAccessor, weightExists := v.Attributes[gltf.WEIGHTS_0]; weightExists {

				weightBuffer := [][4]float32{}
				weights, err := modeler.ReadWeights(doc, doc.Accessors[weightAccessor], weightBuffer)

				if err != nil {
					return nil, err
				}

				boneBuffer := [][4]uint16{}
				bones, err := modeler.ReadJoints(doc, doc.Accessors[v.Attributes[gltf.JOINTS_0]], boneBuffer)

				if err != nil {
					return nil, err
				}

				// Store weights and bones; we don't want to waste space and speed storing bones if their weights are 0
				for w := range weights {
					vWeights := weights[w]
					for i := range vWeights {
						if vWeights[i] > 0 {
							vertexData[w].WeightData = append(vertexData[w].WeightData, vWeights[i])
							vertexData[w].Bones = append(vertexData[w].Bones, bones[w][i])
						}
					}
				}

			}

			indexBuffer := []uint32{}

			indices, err := modeler.ReadIndices(doc, doc.Accessors[*v.Indices], indexBuffer)

			if err != nil {
				return nil, err
			}

			newVerts := []*Vertex{}

			for i := 0; i < len(indices); i++ {
				vd := vertexData[indices[i]]
				newVert := NewVertex(vd.Pos[0], vd.Pos[1], vd.Pos[2], vd.UV[0], vd.UV[1])
				newVert.Color = vd.Color.Clone()
				newVert.Weights = append(newVert.Weights, vd.WeightData...)
				newVerts = append(newVerts, newVert)
				verticesToVertexData[newVert] = vd
			}

			newMesh.AddTriangles(newVerts...)

			if vertexData[0].Normal != nil {

				normals := []vector.Vector{}

				for i := 0; i < len(indices); i += 3 {
					normal := vertexData[indices[i]].Normal
					normal = normal.Add(vertexData[indices[i+1]].Normal)
					normal = normal.Add(vertexData[indices[i+2]].Normal)
					normals = append(normals, normal.Unit())
				}

				for triIndex, tri := range newMesh.Triangles {
					tri.Normal = normals[triIndex]
				}

			}

			newMesh.UpdateBounds()

		}

	}

	for _, gltfAnim := range doc.Animations {
		anim := NewAnimation(gltfAnim.Name)
		collection.Animations[gltfAnim.Name] = anim

		animLength := 0.0

		for _, channel := range gltfAnim.Channels {

			sampler := gltfAnim.Samplers[*channel.Sampler]

			channelName := "root"
			if channel.Target.Node != nil {
				channelName = doc.Nodes[*channel.Target.Node].Name
			}

			animChannel := anim.Channels[channelName]
			if animChannel == nil {
				animChannel = anim.AddChannel(channelName)
			}

			if channel.Target.Path == gltf.TRSTranslation {

				id, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Input], nil)

				if err != nil {
					return nil, err
				}

				inputData := id.([]float32)

				od, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Output], nil)

				if err != nil {
					return nil, err
				}

				outputData := od.([][3]float32)

				track := animChannel.AddTrack(TrackTypePosition)
				for i := 0; i < len(inputData); i++ {
					t := inputData[i]
					p := outputData[i]
					track.AddKeyframe(float64(t), vector.Vector{float64(p[0]), float64(p[1]), float64(p[2])})
					if float64(t) > animLength {
						animLength = float64(t)
					}
				}

			} else if channel.Target.Path == gltf.TRSScale {

				id, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Input], nil)

				if err != nil {
					return nil, err
				}

				inputData := id.([]float32)

				od, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Output], nil)

				if err != nil {
					return nil, err
				}

				outputData := od.([][3]float32)

				track := animChannel.AddTrack(TrackTypeScale)
				for i := 0; i < len(inputData); i++ {
					t := inputData[i]
					p := outputData[i]
					track.AddKeyframe(float64(t), vector.Vector{float64(p[0]), float64(p[1]), float64(p[2])})
					if float64(t) > animLength {
						animLength = float64(t)
					}
				}

			} else if channel.Target.Path == gltf.TRSRotation {

				id, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Input], nil)

				if err != nil {
					return nil, err
				}

				inputData := id.([]float32)

				od, err := modeler.ReadAccessor(doc, doc.Accessors[*sampler.Output], nil)

				if err != nil {
					return nil, err
				}

				outputData := od.([][4]float32)

				track := animChannel.AddTrack(TrackTypeRotation)

				for i := 0; i < len(inputData); i++ {
					t := inputData[i]
					p := outputData[i]
					track.AddKeyframe(float64(t), NewQuaternion(float64(p[0]), float64(p[1]), float64(p[2]), float64(p[3])))
					if float64(t) > animLength {
						animLength = float64(t)
					}
				}

			}

		}

		anim.Length = animLength

	}

	// skins := []*Skin{}

	// for _, skin := range doc.Skins {

	// 	skins = append(skins, skin.)

	// }

	objects := []INode{}

	for _, node := range doc.Nodes {

		var obj INode

		if node.Mesh != nil {
			mesh := collection.Meshes[doc.Meshes[*node.Mesh].Name]
			obj = NewModel(mesh, node.Name)
		} else {
			obj = NewNode(node.Name)
		}

		for _, child := range node.Children {
			obj.AddChildren(objects[int(child)])
		}

		mtData := node.Matrix

		matrix := NewMatrix4()
		matrix = matrix.SetRow(0, vector.Vector{float64(mtData[0]), float64(mtData[1]), float64(mtData[2]), float64(mtData[3])})
		matrix = matrix.SetRow(1, vector.Vector{float64(mtData[4]), float64(mtData[5]), float64(mtData[6]), float64(mtData[7])})
		matrix = matrix.SetRow(2, vector.Vector{float64(mtData[8]), float64(mtData[9]), float64(mtData[10]), float64(mtData[11])})
		matrix = matrix.SetRow(3, vector.Vector{float64(mtData[12]), float64(mtData[13]), float64(mtData[14]), float64(mtData[15])})

		if !matrix.IsIdentity() {

			p, s, r := matrix.Decompose()

			obj.SetLocalPosition(p)
			obj.SetLocalScale(s)
			obj.SetLocalRotation(r)

		} else {
			obj.SetLocalPosition(vector.Vector{float64(node.Translation[0]), float64(node.Translation[1]), float64(node.Translation[2])})
			obj.SetLocalScale(vector.Vector{float64(node.Scale[0]), float64(node.Scale[1]), float64(node.Scale[2])})
			rotMat := NewMatrix4RotateFromQuaternion(NewQuaternion(float64(node.Rotation[0]), float64(node.Rotation[1]), float64(node.Rotation[2]), float64(node.Rotation[3])))
			obj.SetLocalRotation(rotMat)
		}

		objects = append(objects, obj)

	}

	// Do this twice so we can be sure that all of the nodes can be created first
	for i, node := range doc.Nodes {

		// Set up skin for skinning animations
		if node.Skin != nil {

			model := objects[i].(*Model)

			skin := doc.Skins[*node.Skin]

			// Unsure of if this is necessary.
			if skin.Skeleton != nil {
				skeletonRoot := objects[*skin.Skeleton]
				model.SetWorldPosition(skeletonRoot.WorldPosition())
				model.SetWorldScale(skeletonRoot.WorldScale())
				model.SetWorldRotation(skeletonRoot.WorldRotation())
			}

			model.Skinned = true

			allBones := []*Node{}

			for _, b := range skin.Joints {
				allBones = append(allBones, objects[b].(*Node))
			}

			matrices, err := modeler.ReadAccessor(doc, doc.Accessors[*skin.InverseBindMatrices], nil)
			if err != nil {
				return nil, err
			}

			for matIndex, matrix := range matrices.([][4][4]float32) {

				newMat := NewMatrix4()
				for rowIndex, row := range matrix {
					newMat = newMat.SetColumn(rowIndex, vector.Vector{float64(row[0]), float64(row[1]), float64(row[2]), float64(row[3])})
				}

				allBones[matIndex].inverseBindMatrix = newMat

			}

			for _, vertex := range model.Mesh.Vertices {

				model.bones = append(model.bones, []*Node{})

				for _, boneID := range verticesToVertexData[vertex].Bones {
					model.bones[vertex.ID] = append(model.bones[vertex.ID], allBones[boneID])
				}

			}

		}

		for _, childIndex := range node.Children {
			objects[i].AddChildren(objects[int(childIndex)])
		}

	}

	for _, s := range doc.Scenes {

		scene := collection.AddScene(s.Name)

		for _, n := range s.Nodes {
			scene.Root.AddChildren(objects[n])
		}

	}

	return collection, nil

}
